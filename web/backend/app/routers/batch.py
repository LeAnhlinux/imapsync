import asyncio
from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, UploadFile, File
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import BatchRun, SyncJob, User
from ..schemas import BatchRunOut
from ..services.csv_parser import parse_csv
from ..services.imapsync_runner import start_sync

router = APIRouter(prefix="/api/batch", tags=["batch"])


@router.post("/upload", response_model=BatchRunOut, status_code=201)
async def upload(file: UploadFile = File(...), user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    content = await file.read()
    text = content.decode('utf-8', errors='replace')
    rows = parse_csv(text)

    batch = BatchRun(
        user_id=user.id,
        filename=file.filename or "upload.csv",
        total_rows=len(rows),
        status="pending",
    )
    db.add(batch)
    db.commit()
    db.refresh(batch)

    # Store parsed rows in memory for later start
    _batch_data[batch.id] = rows

    return batch


# Temporary in-memory storage for parsed CSV data
_batch_data: dict[int, list[dict]] = {}


@router.post("/{batch_id}/start", response_model=BatchRunOut)
async def start(batch_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    batch = db.query(BatchRun).filter(BatchRun.id == batch_id, BatchRun.user_id == user.id).first()
    if not batch:
        raise HTTPException(status_code=404, detail="Batch not found")

    rows = _batch_data.get(batch_id)
    if not rows:
        raise HTTPException(status_code=400, detail="Batch data expired or not found")

    batch.status = "running"
    db.commit()

    # Run batch in background
    asyncio.create_task(_run_batch(batch.id, user.id, rows, db))

    return batch


async def _run_batch(batch_id: int, user_id: int, rows: list[dict], db: Session):
    from ..database import SessionLocal
    session = SessionLocal()

    try:
        batch = session.query(BatchRun).filter(BatchRun.id == batch_id).first()
        if not batch:
            return

        for row in rows:
            job = SyncJob(
                user_id=user_id,
                status="running",
                started_at=datetime.utcnow(),
            )
            session.add(job)
            session.commit()
            session.refresh(job)

            job.logfile = f"data/logs/{job.id}.txt"
            session.commit()

            async def on_complete(job_id: int, exit_code: int, summary: dict):
                s = SessionLocal()
                try:
                    j = s.query(SyncJob).filter(SyncJob.id == job_id).first()
                    if j:
                        j.status = "completed" if exit_code == 0 else "failed"
                        j.exit_code = exit_code
                        j.finished_at = datetime.utcnow()
                        j.messages_transferred = summary.get("messages_transferred", 0)
                        j.bytes_transferred = summary.get("bytes_transferred", 0)
                        j.errors_count = summary.get("errors_count", 0)
                        j.summary = summary.get("summary", "")
                        s.commit()
                finally:
                    s.close()

            try:
                running_job = await start_sync(
                    job_id=job.id,
                    host1=row["host1"], user1=row["user1"], password1=row["password1"],
                    host2=row["host2"], user2=row["user2"], password2=row["password2"],
                    ssl1=row.get("ssl1", True), ssl2=row.get("ssl2", True),
                    extra_options=row.get("extra_options"),
                    on_complete=on_complete,
                )
                # Wait for it to complete (sequential)
                await running_job.process.wait()
                batch.completed_rows += 1
            except Exception:
                batch.failed_rows += 1

            session.commit()

        batch.status = "completed"
        session.commit()

        _batch_data.pop(batch_id, None)
    finally:
        session.close()


@router.get("/{batch_id}", response_model=BatchRunOut)
def get_batch(batch_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    batch = db.query(BatchRun).filter(BatchRun.id == batch_id, BatchRun.user_id == user.id).first()
    if not batch:
        raise HTTPException(status_code=404, detail="Batch not found")
    return batch
