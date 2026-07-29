
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from ..models import now_vn
from apscheduler.triggers.cron import CronTrigger

from ..database import SessionLocal
from ..models import Schedule

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
    from .sync_starter import start_profile_sync

    db = SessionLocal()
    try:
        schedule = db.query(Schedule).filter(Schedule.id == schedule_id).first()
        if not schedule or not schedule.is_active:
            return

        user_id = schedule.user_id
        profile_id = schedule.profile_id

        schedule.last_run_at = now_vn()
        db.commit()
    finally:
        db.close()

    try:
        await start_profile_sync(user_id, profile_id)
    except LookupError:
        # Profile deleted out from under the schedule; nothing to run.
        pass
