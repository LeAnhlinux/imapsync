
from fastapi import APIRouter, Depends, HTTPException
from ..models import now_vn
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import SyncJob, User
from ..schemas import SyncJobOut, SyncStart
from ..services.imapsync_runner import abort_job, get_job_log
from ..services.sync_starter import start_adhoc_sync, start_profile_sync

router = APIRouter(prefix="/api/sync", tags=["sync"])


@router.post("/start", response_model=SyncJobOut)
async def start(data: SyncStart, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    if data.profile_id:
        try:
            job_id = await start_profile_sync(
                user.id, data.profile_id,
                folders=data.folders, dry_run=data.dry_run,
            )
        except LookupError:
            raise HTTPException(status_code=404, detail="Profile not found")
    else:
        if not all([data.host1, data.user1, data.password1, data.host2, data.user2, data.password2]):
            raise HTTPException(status_code=400, detail="Provide profile_id or all connection params")
        job_id = await start_adhoc_sync(
            user.id,
            host1=data.host1, user1=data.user1, password1=data.password1,
            host2=data.host2, user2=data.user2, password2=data.password2,
            ssl1=data.ssl1, ssl2=data.ssl2,
            extra_options=data.extra_options,
            folders=data.folders, dry_run=data.dry_run,
        )

    return db.query(SyncJob).filter(SyncJob.id == job_id).first()


@router.get("/running", response_model=list[SyncJobOut])
def running(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    jobs = db.query(SyncJob).filter(
        SyncJob.user_id == user.id,
        SyncJob.status == "running",
    ).all()
    return jobs


@router.get("/{job_id}", response_model=SyncJobOut)
def get_job(job_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    job = db.query(SyncJob).filter(SyncJob.id == job_id, SyncJob.user_id == user.id).first()
    if not job:
        raise HTTPException(status_code=404, detail="Job not found")
    return job


@router.post("/{job_id}/abort")
async def abort(job_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    job = db.query(SyncJob).filter(SyncJob.id == job_id, SyncJob.user_id == user.id).first()
    if not job:
        raise HTTPException(status_code=404, detail="Job not found")

    success = await abort_job(job_id)
    if success:
        job.status = "aborted"
        job.finished_at = now_vn()
        db.commit()
        return {"status": "aborted"}
    else:
        raise HTTPException(status_code=400, detail="Job not running or already finished")


@router.get("/{job_id}/log")
def log(job_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    job = db.query(SyncJob).filter(SyncJob.id == job_id, SyncJob.user_id == user.id).first()
    if not job:
        raise HTTPException(status_code=404, detail="Job not found")

    content = get_job_log(job_id)
    if content is None:
        return {"log": ""}
    return {"log": content}
