from fastapi import APIRouter, Depends, Query
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import SyncJob, User
from ..schemas import HistoryStats, SyncJobOut

router = APIRouter(prefix="/api/history", tags=["history"])


@router.get("", response_model=list[SyncJobOut])
def list_history(
    page: int = Query(1, ge=1),
    per_page: int = Query(20, ge=1, le=100),
    status: str | None = None,
    q: str | None = None,
    user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    query = db.query(SyncJob).filter(SyncJob.user_id == user.id)

    if status:
        query = query.filter(SyncJob.status == status)
    if q:
        query = query.filter(SyncJob.summary.ilike(f"%{q}%"))

    query = query.order_by(SyncJob.started_at.desc())
    offset = (page - 1) * per_page
    return query.offset(offset).limit(per_page).all()


@router.get("/stats", response_model=HistoryStats)
def stats(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    base = db.query(SyncJob).filter(SyncJob.user_id == user.id)

    total = base.count()
    successful = base.filter(SyncJob.status == "completed").count()
    failed = base.filter(SyncJob.status == "failed").count()

    agg = base.with_entities(
        func.coalesce(func.sum(SyncJob.messages_transferred), 0),
        func.coalesce(func.sum(SyncJob.bytes_transferred), 0),
    ).first()

    total_messages = agg[0] if agg else 0
    total_bytes = agg[1] if agg else 0
    success_rate = (successful / total * 100) if total > 0 else 0

    return HistoryStats(
        total_syncs=total,
        successful=successful,
        failed=failed,
        total_messages=total_messages,
        total_bytes=total_bytes,
        success_rate=round(success_rate, 1),
    )
