import asyncio
from datetime import datetime

from apscheduler.schedulers.asyncio import AsyncIOScheduler
from apscheduler.triggers.cron import CronTrigger

from ..database import SessionLocal
from ..models import Profile, Schedule, SyncJob
from ..security import decrypt_password

scheduler = AsyncIOScheduler()


def init_scheduler():
    """Load all active schedules from DB and start the scheduler."""
    db = SessionLocal()
    try:
        schedules = db.query(Schedule).filter(Schedule.is_active == True).all()
        for s in schedules:
            _add_job(s)
        scheduler.start()
    finally:
        db.close()


def add_schedule_job(schedule: Schedule):
    _add_job(schedule)


def update_schedule_job(schedule: Schedule):
    job_id = f"schedule_{schedule.id}"
    if scheduler.get_job(job_id):
        scheduler.remove_job(job_id)
    if schedule.is_active:
        _add_job(schedule)


def remove_schedule_job(schedule_id: int):
    job_id = f"schedule_{schedule_id}"
    if scheduler.get_job(job_id):
        scheduler.remove_job(job_id)


def _add_job(schedule: Schedule):
    job_id = f"schedule_{schedule.id}"
    try:
        trigger = CronTrigger.from_crontab(schedule.cron_expression)
        scheduler.add_job(
            _run_scheduled_sync,
            trigger=trigger,
            id=job_id,
            args=[schedule.id],
            replace_existing=True,
        )
    except Exception:
        pass


async def _run_scheduled_sync(schedule_id: int):
    """Execute a scheduled sync job."""
    from .imapsync_runner import start_sync

    db = SessionLocal()
    try:
        schedule = db.query(Schedule).filter(Schedule.id == schedule_id).first()
        if not schedule or not schedule.is_active:
            return

        profile = db.query(Profile).filter(Profile.id == schedule.profile_id).first()
        if not profile:
            return

        # Create job record
        job = SyncJob(
            user_id=schedule.user_id,
            profile_id=profile.id,
            status="running",
            started_at=datetime.utcnow(),
        )
        db.add(job)
        db.commit()
        db.refresh(job)

        job.logfile = f"data/logs/{job.id}.txt"
        db.commit()

        schedule.last_run_at = datetime.utcnow()
        db.commit()

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

        await start_sync(
            job_id=job.id,
            host1=profile.host1, user1=profile.user1,
            password1=decrypt_password(profile.password1_enc),
            host2=profile.host2, user2=profile.user2,
            password2=decrypt_password(profile.password2_enc),
            ssl1=profile.ssl1, ssl2=profile.ssl2,
            extra_options=profile.extra_options,
            on_complete=on_complete,
        )
    finally:
        db.close()
