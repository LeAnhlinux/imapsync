"""Create a SyncJob row and spawn imapsync for it.

Shared by the sync router, the scheduler and the Telegram bot so the
"create the row, decrypt the credentials, spawn, record the pid" sequence
exists once.
"""

from ..database import SessionLocal
from ..models import Profile, SyncJob, format_account, now_vn
from ..security import decrypt_password
from .imapsync_runner import start_sync
from .job_completion import finalize_job


async def _launch(
    user_id: int,
    params: dict,
    profile_id: int | None = None,
    folders: list[str] | None = None,
    dry_run: bool = False,
) -> int:
    db = SessionLocal()
    try:
        job = SyncJob(
            user_id=user_id,
            profile_id=profile_id,
            source_account=format_account(params["user1"], params["host1"]),
            dest_account=format_account(params["user2"], params["host2"]),
            status="running",
            started_at=now_vn(),
        )
        db.add(job)
        db.commit()
        db.refresh(job)

        job.logfile = f"data/logs/{job.id}.txt"
        db.commit()
        job_id = job.id
    finally:
        db.close()

    running_job = await start_sync(
        job_id=job_id,
        on_complete=finalize_job,
        folders=folders or None,
        dry_run=dry_run,
        **params,
    )

    db = SessionLocal()
    try:
        job = db.query(SyncJob).filter(SyncJob.id == job_id).first()
        if job:
            job.pid = running_job.process.pid
            db.commit()
    finally:
        db.close()

    return job_id


async def start_profile_sync(
    user_id: int,
    profile_id: int,
    folders: list[str] | None = None,
    dry_run: bool = False,
) -> int:
    """Start a sync for one stored profile. Returns the new job id.

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

        params = dict(
            host1=profile.host1, user1=profile.user1,
            password1=decrypt_password(profile.password1_enc),
            host2=profile.host2, user2=profile.user2,
            password2=decrypt_password(profile.password2_enc),
            ssl1=profile.ssl1, ssl2=profile.ssl2,
            extra_options=profile.extra_options or {},
        )
    finally:
        db.close()

    return await _launch(user_id, params, profile_id=profile_id, folders=folders, dry_run=dry_run)


async def start_adhoc_sync(
    user_id: int,
    host1: str,
    user1: str,
    password1: str,
    host2: str,
    user2: str,
    password2: str,
    ssl1: bool = True,
    ssl2: bool = True,
    extra_options: dict | None = None,
    folders: list[str] | None = None,
    dry_run: bool = False,
) -> int:
    """Start a sync from credentials given directly, without a stored profile."""
    params = dict(
        host1=host1, user1=user1, password1=password1,
        host2=host2, user2=user2, password2=password2,
        ssl1=ssl1, ssl2=ssl2,
        extra_options=extra_options or {},
    )
    return await _launch(user_id, params, folders=folders, dry_run=dry_run)
