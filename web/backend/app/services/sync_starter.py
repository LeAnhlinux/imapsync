"""Launch a sync from a stored profile.

Shared by the scheduler and the Telegram bot so the "create the SyncJob row,
decrypt the credentials, spawn imapsync" sequence exists once.
"""

from ..database import SessionLocal
from ..models import Profile, SyncJob, now_vn
from ..security import decrypt_password
from .imapsync_runner import start_sync
from .job_completion import finalize_job


async def start_profile_sync(user_id: int, profile_id: int) -> int:
    """Start a sync for one profile. Returns the new job id.

    Raises LookupError if the profile does not exist or belongs to someone else.
    """
    db = SessionLocal()
    try:
        profile = db.query(Profile).filter(
            Profile.id == profile_id,
            Profile.user_id == user_id,
        ).first()
        if not profile:
            raise LookupError(f"Profile {profile_id} not found")

        job = SyncJob(
            user_id=user_id,
            profile_id=profile.id,
            source_account=f"{profile.user1}@{profile.host1}",
            dest_account=f"{profile.user2}@{profile.host2}",
            status="running",
            started_at=now_vn(),
        )
        db.add(job)
        db.commit()
        db.refresh(job)

        job.logfile = f"data/logs/{job.id}.txt"
        db.commit()

        job_id = job.id
        params = dict(
            host1=profile.host1, user1=profile.user1,
            password1=decrypt_password(profile.password1_enc),
            host2=profile.host2, user2=profile.user2,
            password2=decrypt_password(profile.password2_enc),
            ssl1=profile.ssl1, ssl2=profile.ssl2,
            extra_options=profile.extra_options,
        )
    finally:
        db.close()

    await start_sync(job_id=job_id, on_complete=finalize_job, **params)
    return job_id
