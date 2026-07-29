"""Telegram bot, long-polling, running inside the API process.

Only chat ids listed in TELEGRAM_ALLOWED_CHAT_IDS are answered — the bot can
start syncs and read job summaries, so that list is the entire access control
story. Message text is treated as data: nothing but the leading /command is
interpreted, and no message content is ever passed to a shell.

Replies are sent as plain text on purpose. Mailbox names and hosts are full of
underscores and dots that Telegram's Markdown parser chokes on, and escaping
them everywhere is a bug factory.
"""

import asyncio
import logging

import httpx

from ..config import settings
from ..database import SessionLocal
from ..models import Profile, SyncJob, User

log = logging.getLogger("telegram")

_POLL_TIMEOUT = 30
_MAX_MESSAGE = 3500  # Telegram's limit is 4096; leave room for our framing

HELP = """imapsync bot

/profiles - list sync profiles
/sync <profile_id> - start a sync
/jobs - jobs running right now
/status <job_id> - status of one job
/stop <job_id> - abort a running job
/history - last 10 finished jobs
/help - this message"""


def _url(method: str) -> str:
    return f"https://api.telegram.org/bot{settings.TELEGRAM_BOT_TOKEN}/{method}"


def _redact(exc: Exception) -> str:
    """httpx puts the failing URL in its messages, and ours carries the token."""
    text = str(exc)
    token = settings.TELEGRAM_BOT_TOKEN
    return text.replace(token, "<token>") if token else text


async def _call(client: httpx.AsyncClient, method: str, **params):
    response = await client.post(_url(method), json=params)
    response.raise_for_status()
    payload = response.json()
    if not payload.get("ok"):
        raise RuntimeError(f"Telegram API error on {method}: {payload.get('description', payload)}")
    return payload.get("result")


async def send_message(chat_id: int, text: str) -> None:
    if not settings.TELEGRAM_BOT_TOKEN:
        return
    if len(text) > _MAX_MESSAGE:
        text = text[:_MAX_MESSAGE] + "\n... (truncated)"
    try:
        async with httpx.AsyncClient(timeout=20) as client:
            await _call(client, "sendMessage", chat_id=chat_id, text=text)
    except Exception as exc:
        log.warning("Telegram sendMessage to %s failed: %s", chat_id, _redact(exc))


async def notify_job_finished(job_id: int, status: str, summary: str, user_id: int) -> None:
    """Push a job result to every allowed chat, if the bot owns that user."""
    if not settings.telegram_enabled:
        return

    db = SessionLocal()
    try:
        bot_user = db.query(User).filter(User.username == settings.TELEGRAM_USERNAME).first()
        if not bot_user or bot_user.id != user_id:
            return
        job = db.query(SyncJob).filter(SyncJob.id == job_id).first()
        route = f"{job.source_account} -> {job.dest_account}" if job else ""
        messages = job.messages_transferred if job else 0
    finally:
        db.close()

    icon = {"completed": "OK", "failed": "FAILED", "aborted": "ABORTED"}.get(status, status.upper())
    text = f"[{icon}] Job #{job_id}\n{route}\nMessages transferred: {messages}"
    if summary:
        text += f"\n\n{summary.strip()}"

    for chat_id in settings.telegram_chat_ids:
        await send_message(chat_id, text)


# --------------------------------------------------------------------------
# Commands
# --------------------------------------------------------------------------

def _bot_user_id() -> int | None:
    db = SessionLocal()
    try:
        user = db.query(User).filter(User.username == settings.TELEGRAM_USERNAME).first()
        return user.id if user else None
    finally:
        db.close()


def _cmd_profiles(user_id: int) -> str:
    db = SessionLocal()
    try:
        profiles = db.query(Profile).filter(Profile.user_id == user_id).all()
        if not profiles:
            return "No profiles yet. Create one in the web UI first."
        lines = ["Profiles:"]
        for p in profiles:
            # user1 is usually already a full address, so don't glue the host on
            lines.append(
                f"  {p.id}. {p.name}\n"
                f"     from: {p.user1} on {p.host1}\n"
                f"     to:   {p.user2} on {p.host2}"
            )
        lines.append("\nStart one with: /sync <id>")
        return "\n".join(lines)
    finally:
        db.close()


def _cmd_jobs(user_id: int) -> str:
    db = SessionLocal()
    try:
        jobs = db.query(SyncJob).filter(
            SyncJob.user_id == user_id,
            SyncJob.status == "running",
        ).all()
        if not jobs:
            return "Nothing running."
        lines = ["Running:"]
        for j in jobs:
            lines.append(f"  #{j.id} {j.source_account} -> {j.dest_account}")
        return "\n".join(lines)
    finally:
        db.close()


def _cmd_status(user_id: int, job_id: int) -> str:
    from .imapsync_runner import get_running_job

    db = SessionLocal()
    try:
        job = db.query(SyncJob).filter(
            SyncJob.id == job_id,
            SyncJob.user_id == user_id,
        ).first()
        if not job:
            return f"Job #{job_id} not found."
        lines = [
            f"Job #{job.id}: {job.status}",
            f"{job.source_account} -> {job.dest_account}",
            f"Messages: {job.messages_transferred}   Bytes: {job.bytes_transferred}   Errors: {job.errors_count}",
        ]
        if job.started_at:
            lines.append(f"Started: {job.started_at:%Y-%m-%d %H:%M:%S}")
        if job.finished_at:
            lines.append(f"Finished: {job.finished_at:%Y-%m-%d %H:%M:%S}")
        summary = job.summary
    finally:
        db.close()

    running = get_running_job(job_id)
    if running and running.progress:
        p = running.progress
        lines.append(f"Progress: {p.get('percent', 0)}%  folder {p.get('folder', '?')}  ETA {p.get('eta', '?')}")
    elif summary:
        lines.append("")
        lines.append(summary.strip())

    return "\n".join(lines)


def _cmd_history(user_id: int) -> str:
    db = SessionLocal()
    try:
        jobs = db.query(SyncJob).filter(
            SyncJob.user_id == user_id,
        ).order_by(SyncJob.id.desc()).limit(10).all()
        if not jobs:
            return "No jobs yet."
        lines = ["Last 10 jobs:"]
        for j in jobs:
            when = f"{j.started_at:%m-%d %H:%M}" if j.started_at else "-"
            lines.append(f"  #{j.id} [{j.status}] {when}  {j.source_account} -> {j.dest_account}")
        return "\n".join(lines)
    finally:
        db.close()


async def _cmd_sync(user_id: int, profile_id: int) -> str:
    from .sync_starter import start_profile_sync

    try:
        job_id = await start_profile_sync(user_id, profile_id)
    except LookupError:
        return f"Profile {profile_id} not found. Use /profiles to list them."
    except Exception as exc:
        log.exception("Telegram /sync failed")
        return f"Could not start the sync: {exc}"
    return f"Started job #{job_id}. I will message you when it finishes.\nCheck with: /status {job_id}"


async def _cmd_stop(user_id: int, job_id: int) -> str:
    from .imapsync_runner import abort_job
    from ..models import now_vn

    db = SessionLocal()
    try:
        job = db.query(SyncJob).filter(
            SyncJob.id == job_id,
            SyncJob.user_id == user_id,
        ).first()
        if not job:
            return f"Job #{job_id} not found."
    finally:
        db.close()

    if not await abort_job(job_id):
        return f"Job #{job_id} is not running."

    db = SessionLocal()
    try:
        job = db.query(SyncJob).filter(SyncJob.id == job_id).first()
        if job:
            job.status = "aborted"
            job.finished_at = now_vn()
            db.commit()
    finally:
        db.close()
    return f"Job #{job_id} aborted."


async def _dispatch(text: str) -> str:
    user_id = _bot_user_id()
    if user_id is None:
        return (
            f"TELEGRAM_USERNAME is set to '{settings.TELEGRAM_USERNAME}' "
            "but no such user exists. Create it with scripts/create_admin.py."
        )

    parts = text.strip().split()
    if not parts:
        return HELP
    # Telegram appends @botname to commands in groups
    command = parts[0].split("@")[0].lower()
    args = parts[1:]

    def _int_arg(name: str) -> int | None:
        if not args:
            return None
        try:
            return int(args[0])
        except ValueError:
            return None

    if command in ("/start", "/help"):
        return HELP
    if command == "/profiles":
        return _cmd_profiles(user_id)
    if command == "/jobs":
        return _cmd_jobs(user_id)
    if command == "/history":
        return _cmd_history(user_id)
    if command == "/sync":
        profile_id = _int_arg("profile")
        if profile_id is None:
            return "Usage: /sync <profile_id>   (see /profiles)"
        return await _cmd_sync(user_id, profile_id)
    if command == "/status":
        job_id = _int_arg("job")
        if job_id is None:
            return "Usage: /status <job_id>"
        return _cmd_status(user_id, job_id)
    if command == "/stop":
        job_id = _int_arg("job")
        if job_id is None:
            return "Usage: /stop <job_id>"
        return await _cmd_stop(user_id, job_id)

    return f"Unknown command: {command}\n\n{HELP}"


# --------------------------------------------------------------------------
# Polling loop
# --------------------------------------------------------------------------

async def _handle_update(update: dict) -> None:
    message = update.get("message") or update.get("edited_message")
    if not message:
        return

    chat_id = message.get("chat", {}).get("id")
    if chat_id not in settings.telegram_chat_ids:
        log.warning("Ignoring Telegram message from unauthorized chat %s", chat_id)
        return

    text = message.get("text")
    if not text:
        return

    try:
        reply = await _dispatch(text)
    except Exception as exc:
        log.exception("Telegram command failed")
        reply = f"Something went wrong: {exc}"

    await send_message(chat_id, reply)


async def run_bot() -> None:
    """Long-poll getUpdates until cancelled."""
    offset = None
    backoff = 1

    async with httpx.AsyncClient(timeout=_POLL_TIMEOUT + 15) as client:
        try:
            me = await _call(client, "getMe")
            log.info("Telegram bot connected as @%s", me.get("username"))
            backoff = 1
        except Exception as exc:
            log.error("Telegram bot could not authenticate, giving up: %s", _redact(exc))
            return

        while True:
            try:
                params = {"timeout": _POLL_TIMEOUT}
                if offset is not None:
                    params["offset"] = offset
                updates = await _call(client, "getUpdates", **params) or []
                backoff = 1

                for update in updates:
                    offset = update["update_id"] + 1
                    await _handle_update(update)

            except asyncio.CancelledError:
                raise
            except Exception as exc:
                log.warning("Telegram polling error (retry in %ss): %s", backoff, _redact(exc))
                await asyncio.sleep(backoff)
                backoff = min(backoff * 2, 60)
