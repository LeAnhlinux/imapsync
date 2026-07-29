"""Single place where a finished sync job is written back to the database.

sync/batch/scheduler all used to carry their own copy of this callback, which is
how the abort status ended up being overwritten by "failed" and how the
notification settings ended up never being read.
"""

from ..database import SessionLocal
from ..models import NotificationSettings, SyncJob, now_vn
from ..security import decrypt_password
from .notifier import send_sync_notification


async def finalize_job(job_id: int, exit_code: int, summary: dict) -> None:
    session = SessionLocal()
    try:
        job = session.query(SyncJob).filter(SyncJob.id == job_id).first()
        if not job:
            return

        if summary.get("aborted") or job.status == "aborted":
            # A user-requested SIGTERM makes imapsync exit non-zero; that is not
            # a failure and must not overwrite the status set by /abort.
            job.status = "aborted"
        else:
            job.status = "completed" if exit_code == 0 else "failed"

        job.exit_code = exit_code
        job.finished_at = now_vn()
        job.messages_transferred = summary.get("messages_transferred", 0)
        job.bytes_transferred = summary.get("bytes_transferred", 0)
        job.errors_count = summary.get("errors_count", 0)
        job.summary = summary.get("summary", "")
        session.commit()

        status = job.status
        user_id = job.user_id
        job_summary = job.summary or ""
    finally:
        session.close()

    await _notify(user_id, job_id, status, job_summary)


async def _notify(user_id: int, job_id: int, status: str, summary: str) -> None:
    if status == "aborted":
        return

    session = SessionLocal()
    try:
        settings = session.query(NotificationSettings).filter(
            NotificationSettings.user_id == user_id
        ).first()

        if not settings or not settings.smtp_host or not settings.to_email:
            return
        if status == "completed" and not settings.notify_on_success:
            return
        if status == "failed" and not settings.notify_on_failure:
            return

        smtp_password = (
            decrypt_password(settings.smtp_password_enc)
            if settings.smtp_password_enc else None
        )
        smtp_host = settings.smtp_host
        smtp_port = settings.smtp_port
        smtp_user = settings.smtp_user
        from_email = settings.from_email or settings.smtp_user or ""
        to_email = settings.to_email
    finally:
        session.close()

    await send_sync_notification(
        smtp_host=smtp_host,
        smtp_port=smtp_port,
        smtp_user=smtp_user,
        smtp_password=smtp_password,
        from_email=from_email,
        to_email=to_email,
        job_id=job_id,
        status=status,
        summary=summary,
    )


def reconcile_orphan_jobs() -> int:
    """Mark jobs left "running" by a crash or restart as interrupted.

    The registry of running subprocesses lives in memory, so a restart loses
    track of them and the rows would otherwise stay "running" forever.
    """
    session = SessionLocal()
    try:
        orphans = session.query(SyncJob).filter(SyncJob.status == "running").all()
        for job in orphans:
            job.status = "interrupted"
            job.finished_at = now_vn()
            job.summary = (job.summary or "") + "\nJob interrupted: the server restarted while it was running."
        session.commit()
        return len(orphans)
    finally:
        session.close()
