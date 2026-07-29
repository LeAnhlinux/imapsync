import asyncio

from fastapi import APIRouter, Depends, HTTPException, UploadFile, File, Query
from ..models import now_vn
from pydantic import BaseModel
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import BatchRun, SyncJob, User
from ..schemas import BatchRunOut
from ..security import decrypt_password, encrypt_password
from ..services.csv_parser import parse_csv
from ..services.imapsync_runner import start_sync
from ..services.job_completion import finalize_job

router = APIRouter(prefix="/api/batch", tags=["batch"])


class CsvTextInput(BaseModel):
    csv_text: str


@router.post("/upload", response_model=BatchRunOut, status_code=201)
async def upload(
    file: UploadFile = File(...),
    dry_run: bool = Query(False),
    ssl1: bool = Query(True),
    ssl2: bool = Query(True),
    automap: bool = Query(True),
    usecache: bool = Query(True),
    user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    content = await file.read()
    text = content.decode('utf-8', errors='replace')
    return _create_batch(text, file.filename or "upload.csv", dry_run, ssl1, ssl2, automap, usecache, user, db)


@router.post("/upload-text", response_model=BatchRunOut, status_code=201)
async def upload_text(
    data: CsvTextInput,
    dry_run: bool = Query(False),
    ssl1: bool = Query(True),
    ssl2: bool = Query(True),
    automap: bool = Query(True),
    usecache: bool = Query(True),
    user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    return _create_batch(data.csv_text, "paste.csv", dry_run, ssl1, ssl2, automap, usecache, user, db)


def _create_batch(text: str, filename: str, dry_run: bool, ssl1: bool, ssl2: bool, automap: bool, usecache: bool, user: User, db: Session):
    rows = parse_csv(text)
    if not rows:
        raise HTTPException(status_code=400, detail="No valid rows found in CSV data")

    # batch_data lands in the database, so the IMAP passwords get the same
    # Fernet treatment as the ones stored on a Profile.
    for row in rows:
        row["password1"] = encrypt_password(row["password1"])
        row["password2"] = encrypt_password(row["password2"])

    batch = BatchRun(
        user_id=user.id,
        filename=filename,
        total_rows=len(rows),
        status="pending",
        batch_data={
            "rows": rows,
            "dry_run": dry_run,
            "ssl1": ssl1,
            "ssl2": ssl2,
            "automap": automap,
            "usecache": usecache,
        },
    )
    db.add(batch)
    db.commit()
    db.refresh(batch)

    return batch


@router.post("/{batch_id}/start", response_model=BatchRunOut)
async def start(batch_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    batch = db.query(BatchRun).filter(BatchRun.id == batch_id, BatchRun.user_id == user.id).first()
    if not batch:
        raise HTTPException(status_code=404, detail="Batch not found")

    if not batch.batch_data:
        raise HTTPException(status_code=400, detail="Batch data not found")

    batch.status = "running"
    db.commit()

    # Run batch in background
    asyncio.create_task(_run_batch(batch.id, user.id, batch.batch_data))

    return batch


async def _run_batch(batch_id: int, user_id: int, data: dict):
    from ..database import SessionLocal

    rows = data["rows"]
    dry_run = data.get("dry_run", False)
    default_ssl1 = data.get("ssl1", True)
    default_ssl2 = data.get("ssl2", True)
    automap = data.get("automap", True)
    usecache = data.get("usecache", True)

    session = SessionLocal()

    try:
        batch = session.query(BatchRun).filter(BatchRun.id == batch_id).first()
        if not batch:
            return

        for row in rows:
            job = SyncJob(
                user_id=user_id,
                source_account=f"{row['user1']}@{row['host1']}",
                dest_account=f"{row['user2']}@{row['host2']}",
                status="running",
                started_at=now_vn(),
            )
            session.add(job)
            session.commit()
            session.refresh(job)

            job.logfile = f"data/logs/{job.id}.txt"
            session.commit()

            try:
                # Merge global options with per-row extra_options
                extra = dict(row.get("extra_options") or {})
                if automap:
                    extra["automap"] = True
                if usecache:
                    extra["usecache"] = True

                running_job = await start_sync(
                    job_id=job.id,
                    host1=row["host1"], user1=row["user1"],
                    password1=decrypt_password(row["password1"]),
                    host2=row["host2"], user2=row["user2"],
                    password2=decrypt_password(row["password2"]),
                    ssl1=row.get("ssl1", default_ssl1),
                    ssl2=row.get("ssl2", default_ssl2),
                    extra_options=extra,
                    dry_run=dry_run,
                    on_complete=finalize_job,
                )
                # Wait for it to complete (sequential)
                await running_job.process.wait()
                batch.completed_rows += 1
            except Exception:
                batch.failed_rows += 1

            session.commit()

        batch.status = "completed"
        # Clear batch_data after completion to save space
        batch.batch_data = None
        session.commit()

    finally:
        session.close()


@router.get("/{batch_id}", response_model=BatchRunOut)
def get_batch(batch_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    batch = db.query(BatchRun).filter(BatchRun.id == batch_id, BatchRun.user_id == user.id).first()
    if not batch:
        raise HTTPException(status_code=404, detail="Batch not found")
    return batch
