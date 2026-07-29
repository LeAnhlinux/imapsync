
from fastapi import APIRouter, Depends, HTTPException
from ..models import now_vn
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import Profile, SyncJob, User
from ..schemas import SyncJobOut, SyncStart
from ..security import decrypt_password
from ..services.imapsync_runner import abort_job, get_job_log, start_sync
from ..services.job_completion import finalize_job

router = APIRouter(prefix="/api/sync", tags=["sync"])


@router.post("/start", response_model=SyncJobOut)
async def start(data: SyncStart, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    # Resolve credentials from profile or direct params
    if data.profile_id:
        profile = db.query(Profile).filter(Profile.id == data.profile_id, Profile.user_id == user.id).first()
        if not profile:
            raise HTTPException(status_code=404, detail="Profile not found")
        host1 = profile.host1
        user1 = profile.user1
        password1 = decrypt_password(profile.password1_enc)
        ssl1 = profile.ssl1
        host2 = profile.host2
        user2 = profile.user2
        password2 = decrypt_password(profile.password2_enc)
        ssl2 = profile.ssl2
        extra_options = profile.extra_options or {}
    else:
        if not all([data.host1, data.user1, data.password1, data.host2, data.user2, data.password2]):
            raise HTTPException(status_code=400, detail="Provide profile_id or all connection params")
        host1 = data.host1
        user1 = data.user1
        password1 = data.password1
        ssl1 = data.ssl1
        host2 = data.host2
        user2 = data.user2
        password2 = data.password2
        ssl2 = data.ssl2
        extra_options = data.extra_options

    # Create DB record
    job = SyncJob(
        user_id=user.id,
        profile_id=data.profile_id,
        source_account=f"{user1}@{host1}",
        dest_account=f"{user2}@{host2}",
        status="running",
        started_at=now_vn(),
        logfile=f"{None}",  # Will be set after start
    )
    db.add(job)
    db.commit()
    db.refresh(job)

    job.logfile = str(f"data/logs/{job.id}.txt")
    db.commit()

    # Start the subprocess
    running_job = await start_sync(
        job_id=job.id,
        host1=host1, user1=user1, password1=password1,
        host2=host2, user2=user2, password2=password2,
        ssl1=ssl1, ssl2=ssl2,
        folders=data.folders or None,
        extra_options=extra_options,
        dry_run=data.dry_run,
        on_complete=finalize_job,
    )

    job.pid = running_job.process.pid
    db.commit()
    db.refresh(job)

    return job


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
