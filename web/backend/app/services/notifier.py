import aiosmtplib
from email.message import EmailMessage


async def send_test_email(
    smtp_host: str,
    smtp_port: int,
    smtp_user: str | None,
    smtp_password: str | None,
    from_email: str,
    to_email: str,
) -> bool:
    """Send a test email to verify SMTP settings."""
    try:
        msg = EmailMessage()
        msg["Subject"] = "imapsync Web UI - Test Notification"
        msg["From"] = from_email
        msg["To"] = to_email
        msg.set_content("This is a test notification from imapsync Web UI. Your SMTP settings are working correctly.")

        await aiosmtplib.send(
            msg,
            hostname=smtp_host,
            port=smtp_port,
            username=smtp_user,
            password=smtp_password,
            start_tls=True,
        )
        return True
    except Exception:
        return False


async def send_sync_notification(
    smtp_host: str,
    smtp_port: int,
    smtp_user: str | None,
    smtp_password: str | None,
    from_email: str,
    to_email: str,
    job_id: int,
    status: str,
    summary: str,
) -> bool:
    """Send a sync completion notification."""
    try:
        subject = f"imapsync Job #{job_id} - {'Completed' if status == 'completed' else 'Failed'}"
        body = f"Sync Job #{job_id} has {status}.\n\nSummary:\n{summary}"

        msg = EmailMessage()
        msg["Subject"] = subject
        msg["From"] = from_email
        msg["To"] = to_email
        msg.set_content(body)

        await aiosmtplib.send(
            msg,
            hostname=smtp_host,
            port=smtp_port,
            username=smtp_user,
            password=smtp_password,
            start_tls=True,
        )
        return True
    except Exception:
        return False
