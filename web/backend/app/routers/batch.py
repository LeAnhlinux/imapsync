import asyncio

from fastapi import APIRouter, Depends, HTTPException, UploadFile, File, Query
from pydantic import BaseModel
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import BatchRun, User
from ..schemas import BatchRunOut
from ..services.batch_runner import create_batch, run_batch

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
    return _create(text, file.filename or "upload.csv", dry_run, ssl1, ssl2, automap, usecache, user, db)


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
    return _create(data.csv_text, "paste.csv", dry_run, ssl1, ssl2, automap, usecache, user, db)


def _create(text, filename, dry_run, ssl1, ssl2, automap, usecache, user, db):
    try:
        return create_batch(
            db, user.id, text, filename,
            {"dry_run": dry_run, "ssl1": ssl1, "ssl2": ssl2, "automap": automap, "usecache": usecache},
        )
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc))


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
    asyncio.create_task(run_batch(batch.id, user.id, batch.batch_data))

    return batch


@router.get("/{batch_id}", response_model=BatchRunOut)
def get_batch(batch_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    batch = db.query(BatchRun).filter(BatchRun.id == batch_id, BatchRun.user_id == user.id).first()
    if not batch:
        raise HTTPException(status_code=404, detail="Batch not found")
    return batch
