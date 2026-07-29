"""Telegram bot, long-polling, running inside the API process.

Only chat ids in the allowed list are answered — the bot can start syncs and
read job summaries, so that list is the entire access control story. Message
text is treated as data: nothing but the leading /command is interpreted, and
no message content is ever passed to a shell.

Configuration lives in the telegram_settings table (edited from the Settings
page), seeded once from the TELEGRAM_* environment variables if they were set
before this table existed.

Replies are sent as plain text on purpose. Mailbox names and hosts are full of
underscores and dots that Telegram's Markdown parser chokes on, and escaping
them everywhere is a bug factory.
"""

import asyncio
import logging
from dataclasses import dataclass, field

import httpx

from ..config import settings
from ..database import SessionLocal
from ..models import BatchRun, Profile, SyncJob, TelegramSettings, User, now_vn
from ..security import decrypt_password, encrypt_password
from .imapsync_options import sanitize_extra_options

log = logging.getLogger("telegram")

_POLL_TIMEOUT = 30
_MAX_MESSAGE = 3500  # Telegram's limit is 4096; leave room for our framing
_MAX_CSV_BYTES = 1_000_000

HELP = """imapsync bot

/profiles - list sync profiles
/sync <profile_id> - start a sync from a profile
/run - sync one mailbox now (send /run on its own for the format)
/batch - send a .csv file with this as the caption
/batchgo <batch_id> - run a batch you uploaded
/jobs - jobs running right now
/status <job_id> - status of one job
/stop <job_id> - abort a running job
/history - last 10 finished jobs
/help - this message"""


@dataclass
class BotConfig:
    token: str = ""
    chat_ids: set[int] = field(default_factory=set)
    user_id: int | None = None
    notify_on_success: bool = True
    notify_on_failure: bool = True
    notify_on_progress: bool = True
    progress_step: int = 25

    @property
    def usable(self) -> bool:
        return bool(self.token and self.chat_ids and self.user_id)


# Current config and the polling task, owned by start_bot/stop_bot.
_config = BotConfig()
_task: asyncio.Task | None = None
_status: dict = {"running": False, "bot_username": None, "error": None}
_last_milestone: dict[int, int] = {}


def parse_chat_ids(raw: str | None) -> set[int]:
    ids = set()
    for part in (raw or "").split(","):
        part = part.strip()
        if part:
            try:
                ids.add(int(part))
            except ValueError:
                continue
    return ids


def _seed_from_env(db) -> TelegramSettings | None:
    """One-time import of the old TELEGRAM_* env vars into the new table."""
    if not (settings.TELEGRAM_BOT_TOKEN and settings.TELEGRAM_USERNAME):
        return None
    user = db.query(User).filter(User.username == settings.TELEGRAM_USERNAME).first()
    if not user:
        return None
    row = TelegramSettings(
        id=1,
        enabled=True,
        bot_token_enc=encrypt_password(settings.TELEGRAM_BOT_TOKEN),
        allowed_chat_ids=settings.TELEGRAM_ALLOWED_CHAT_IDS,
        user_id=user.id,
    )
    db.add(row)
    db.commit()
    db.refresh(row)
    log.info("Imported TELEGRAM_* environment settings into telegram_settings")
    return row


def get_settings_row(db, create: bool = False) -> TelegramSettings | None:
    row = db.query(TelegramSettings).filter(TelegramSettings.id == 1).first()
    if row is None:
        row = _seed_from_env(db)
    if row is None and create:
        row = TelegramSettings(id=1, enabled=False)
        db.add(row)
        db.commit()
        db.refresh(row)
    return row


def load_config() -> BotConfig:
    db = SessionLocal()
    try:
        row = get_settings_row(db)
        if not row or not row.enabled or not row.bot_token_enc:
            return BotConfig()
        try:
            token = decrypt_password(row.bot_token_enc)
        except Exception:
            log.error("Stored Telegram token could not be decrypted; disabling the bot")
            return BotConfig()
        return BotConfig(
            token=token,
            chat_ids=parse_chat_ids(row.allowed_chat_ids),
            user_id=row.user_id,
            notify_on_success=row.notify_on_success,
            notify_on_failure=row.notify_on_failure,
            notify_on_progress=row.notify_on_progress,
            progress_step=max(1, row.progress_step or 25),
        )
    finally:
        db.close()


def get_status() -> dict:
    return {**_status, "configured": _config.usable}


# --------------------------------------------------------------------------
# Telegram API
# --------------------------------------------------------------------------

def _url(method: str, token: str | None = None) -> str:
    return f"https://api.telegram.org/bot{token or _config.token}/{method}"


def _redact(exc: Exception, token: str | None = None) -> str:
    """httpx puts the failing URL in its messages, and ours carries the token."""
    text = str(exc)
    for secret in filter(None, (token, _config.token)):
        text = text.replace(secret, "<token>")
    return text


async def _call(client: httpx.AsyncClient, method: str, token: str | None = None, **params):
    response = await client.post(_url(method, token), json=params)
    response.raise_for_status()
    payload = response.json()
    if not payload.get("ok"):
        raise RuntimeError(f"Telegram API error on {method}: {payload.get('description', payload)}")
    return payload.get("result")


async def send_message(chat_id: int, text: str, token: str | None = None) -> bool:
    token = token or _config.token
    if not token:
        return False
    if len(text) > _MAX_MESSAGE:
        text = text[:_MAX_MESSAGE] + "\n... (truncated)"
    try:
        async with httpx.AsyncClient(timeout=20) as client:
            await _call(client, "sendMessage", token=token, chat_id=chat_id, text=text)
        return True
    except Exception as exc:
        log.warning("Telegram sendMessage to %s failed: %s", chat_id, _redact(exc, token))
        return False


async def send_test_message(token: str, chat_ids: set[int]) -> tuple[bool, str]:
    """Used by the Settings page before the bot is switched on."""
    if not chat_ids:
        return False, "No chat ids configured"
    try:
        async with httpx.AsyncClient(timeout=20) as client:
            me = await _call(client, "getMe", token=token)
            for chat_id in chat_ids:
                await _call(
                    client, "sendMessage", token=token, chat_id=chat_id,
                    text="imapsync Web UI: test message. The bot can reach this chat.",
                )
        return True, f"Sent as @{me.get('username')}"
    except Exception as exc:
        return False, _redact(exc, token)


async def _delete_message(chat_id: int, message_id: int) -> None:
    try:
        async with httpx.AsyncClient(timeout=20) as client:
            await _call(client, "deleteMessage", chat_id=chat_id, message_id=message_id)
    except Exception as exc:
        log.info("Could not delete the uploaded CSV message: %s", _redact(exc))


async def _download_file(file_id: str) -> bytes | None:
    async with httpx.AsyncClient(timeout=60) as client:
        info = await _call(client, "getFile", file_id=file_id)
        size = info.get("file_size") or 0
        if size > _MAX_CSV_BYTES:
            return None
        path = info["file_path"]
        response = await client.get(f"https://api.telegram.org/file/bot{_config.token}/{path}")
        response.raise_for_status()
        return response.content


# --------------------------------------------------------------------------
# Notifications
# --------------------------------------------------------------------------

async def notify_job_finished(job_id: int, status: str, summary: str, user_id: int) -> None:
    _last_milestone.pop(job_id, None)

    if not _config.usable or user_id != _config.user_id:
        return
    if status == "completed" and not _config.notify_on_success:
        return
    if status == "failed" and not _config.notify_on_failure:
        return

    db = SessionLocal()
    try:
        job = db.query(SyncJob).filter(SyncJob.id == job_id).first()
        route = f"{job.source_account} -> {job.dest_account}" if job else ""
        messages = job.messages_transferred if job else 0
    finally:
        db.close()

    label = {"completed": "OK", "failed": "FAILED", "aborted": "ABORTED"}.get(status, status.upper())
    text = f"[{label}] Job #{job_id}\n{route}\nMessages transferred: {messages}"
    if summary:
        text += f"\n\n{summary.strip()}"

    for chat_id in _config.chat_ids:
        await send_message(chat_id, text)


async def on_progress(job_id: int, percent: float, folder: str) -> None:
    """Progress listener registered with imapsync_runner.

    imapsync reports progress per folder, which can be many times a second on a
    small mailbox, so only cross-a-step transitions are announced.
    """
    if not _config.usable or not _config.notify_on_progress:
        return

    step = _config.progress_step
    reached = int(percent // step) * step
    if reached <= 0 or reached >= 100:
        return
    if _last_milestone.get(job_id, 0) >= reached:
        return

    db = SessionLocal()
    try:
        job = db.query(SyncJob).filter(SyncJob.id == job_id).first()
        if not job or job.user_id != _config.user_id:
            return
        route = f"{job.source_account} -> {job.dest_account}"
    finally:
        db.close()

    _last_milestone[job_id] = reached
    text = f"[{reached}%] Job #{job_id}\n{route}\nFolder {folder}"
    for chat_id in _config.chat_ids:
        await send_message(chat_id, text)


# --------------------------------------------------------------------------
# Commands
# --------------------------------------------------------------------------

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
    return f"Started job #{job_id}. I will message you as it progresses.\nCheck with: /status {job_id}"


async def _cmd_stop(user_id: int, job_id: int) -> str:
    from .imapsync_runner import abort_job

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


async def _cmd_run(user_id: int, body: str, chat_id: int, message_id: int) -> str:
    """Sync one mailbox from credentials typed straight into the chat."""
    from .batch_runner import create_batch, describe_batch
    from .run_command import USAGE, RunSyntaxError, describe, is_labeled, parse_flat, parse_labeled

    if not body.strip():
        return USAGE

    # Delete before parsing, not after: a message that fails to parse can still
    # carry a working password, and leaving the typo'd ones in the chat is the
    # opposite of the point.
    await _delete_message(chat_id, message_id)

    labeled = is_labeled(body)
    try:
        specs = [parse_labeled(body)] if labeled else parse_flat(body)
    except RunSyntaxError as exc:
        return str(exc)

    if not specs:
        return "I could not parse that.\n\n" + USAGE

    # Several flat lines are a list of mailboxes, not a one-off: make a batch
    # and make the user confirm it.
    if len(specs) > 1:
        db = SessionLocal()
        try:
            batch = create_batch(db, user_id, body, "telegram-run.csv")
            preview = describe_batch(batch)
            batch_id = batch.id
        except ValueError as exc:
            return str(exc)
        finally:
            db.close()
        return (
            f"That was {len(specs)} mailboxes, so I made a batch instead.\n\n"
            f"{preview}\n\n"
            f"{_CREDENTIAL_WARNING}\n"
            f"Run it with: /batchgo {batch_id}"
        )

    spec = specs[0]
    extra = dict(spec.extra_options)
    # ssl1/ssl2 are managed by the runner and blocked from extra_options, so
    # accept them here as their own thing.
    ssl1 = not extra.pop("nossl1", False)
    ssl2 = not extra.pop("nossl2", False)
    dry_run = bool(extra.pop("dry", False))

    _, rejected = sanitize_extra_options(extra)
    if rejected:
        return f"Unsupported or unsafe imapsync options: {', '.join(sorted(rejected))}"

    try:
        job_id = await _start_adhoc(user_id, spec, ssl1, ssl2, extra, dry_run)
    except Exception as exc:
        log.exception("Telegram /run failed")
        return f"Could not start the sync: {exc}"

    return (
        f"Started job #{job_id}\n"
        f"{describe(spec, dry_run)}\n\n"
        f"{_CREDENTIAL_WARNING}\n"
        f"Follow it with: /status {job_id}"
    )


async def _start_adhoc(user_id, spec, ssl1, ssl2, extra, dry_run) -> int:
    from .sync_starter import start_adhoc_sync

    return await start_adhoc_sync(
        user_id,
        host1=spec.host1, user1=spec.user1, password1=spec.password1,
        host2=spec.host2, user2=spec.user2, password2=spec.password2,
        ssl1=ssl1, ssl2=ssl2,
        extra_options=extra,
        dry_run=dry_run,
    )


_CREDENTIAL_WARNING = (
    "I deleted your message from this chat, but Telegram still has a copy on their servers."
)


async def _cmd_batch_upload(user_id: int, document: dict, chat_id: int, message_id: int) -> str:
    """Ingest a CSV sent as a Telegram document."""
    from .batch_runner import create_batch, describe_batch

    filename = document.get("file_name") or "telegram.csv"
    try:
        content = await _download_file(document["file_id"])
    except Exception as exc:
        log.exception("Telegram CSV download failed")
        return f"Could not download the file: {_redact(exc)}"

    if content is None:
        return f"That file is larger than {_MAX_CSV_BYTES // 1000} kB. Split it or use the web UI."

    # The uploaded file holds mailbox passwords; take it out of the chat as
    # soon as it is parsed. Telegram still keeps it on their servers.
    await _delete_message(chat_id, message_id)

    db = SessionLocal()
    try:
        try:
            batch = create_batch(db, user_id, content.decode("utf-8", errors="replace"), filename)
        except ValueError as exc:
            return str(exc)
        preview = describe_batch(batch)
        batch_id = batch.id
    finally:
        db.close()

    return (
        f"{preview}\n\n"
        f"{_CREDENTIAL_WARNING}\n"
        f"Run it with: /batchgo {batch_id}"
    )


async def _cmd_batch_go(user_id: int, batch_id: int) -> str:
    from .batch_runner import run_batch

    db = SessionLocal()
    try:
        batch = db.query(BatchRun).filter(
            BatchRun.id == batch_id,
            BatchRun.user_id == user_id,
        ).first()
        if not batch:
            return f"Batch #{batch_id} not found."
        if batch.status == "running":
            return f"Batch #{batch_id} is already running."
        if not batch.batch_data:
            return f"Batch #{batch_id} has no data left (already finished)."
        batch.status = "running"
        db.commit()
        data = batch.batch_data
        total = batch.total_rows
    finally:
        db.close()

    asyncio.create_task(run_batch(batch_id, user_id, data))
    return f"Batch #{batch_id} started: {total} mailbox(es), one at a time."


async def _dispatch(message: dict) -> str:
    user_id = _config.user_id
    if user_id is None:
        return "The bot has no app user configured. Set one on the Settings page."

    chat_id = message.get("chat", {}).get("id")
    document = message.get("document")
    text = (message.get("text") or message.get("caption") or "").strip()
    parts = text.split()
    # Telegram appends @botname to commands in groups
    command = parts[0].split("@")[0].lower() if parts else ""
    args = parts[1:]

    if document is not None:
        if command and command != "/batch":
            return f"I only accept files with the /batch caption. Got: {command}"
        return await _cmd_batch_upload(user_id, document, chat_id, message["message_id"])

    def _int_arg() -> int | None:
        if not args:
            return None
        try:
            return int(args[0])
        except ValueError:
            return None

    if not command:
        return HELP
    if command in ("/start", "/help"):
        return HELP
    if command == "/profiles":
        return _cmd_profiles(user_id)
    if command == "/jobs":
        return _cmd_jobs(user_id)
    if command == "/history":
        return _cmd_history(user_id)
    if command == "/batch":
        return "Send me a .csv file with /batch as its caption.\n\nFormat (semicolon separated):\nhost1;user1;password1;host2;user2;password2"
    if command == "/sync":
        profile_id = _int_arg()
        if profile_id is None:
            return "Usage: /sync <profile_id>   (see /profiles)"
        return await _cmd_sync(user_id, profile_id)
    if command == "/run":
        # Everything after the command, newlines included, is CSV.
        body = text[len(parts[0]):].strip()
        return await _cmd_run(user_id, body, chat_id, message["message_id"])
    if command == "/status":
        job_id = _int_arg()
        if job_id is None:
            return "Usage: /status <job_id>"
        return _cmd_status(user_id, job_id)
    if command == "/stop":
        job_id = _int_arg()
        if job_id is None:
            return "Usage: /stop <job_id>"
        return await _cmd_stop(user_id, job_id)
    if command == "/batchgo":
        batch_id = _int_arg()
        if batch_id is None:
            return "Usage: /batchgo <batch_id>"
        return await _cmd_batch_go(user_id, batch_id)

    return f"Unknown command: {command}\n\n{HELP}"


# --------------------------------------------------------------------------
# Polling loop and lifecycle
# --------------------------------------------------------------------------

async def _handle_update(update: dict) -> None:
    message = update.get("message") or update.get("edited_message")
    if not message:
        return

    chat_id = message.get("chat", {}).get("id")
    if chat_id not in _config.chat_ids:
        log.warning("Ignoring Telegram message from unauthorized chat %s", chat_id)
        return

    if not (message.get("text") or message.get("caption") or message.get("document")):
        return

    try:
        reply = await _dispatch(message)
    except Exception as exc:
        log.exception("Telegram command failed")
        reply = f"Something went wrong: {_redact(exc)}"

    await send_message(chat_id, reply)


async def _run_loop() -> None:
    offset = None
    backoff = 1

    async with httpx.AsyncClient(timeout=_POLL_TIMEOUT + 15) as client:
        try:
            me = await _call(client, "getMe")
            _status.update(running=True, bot_username=me.get("username"), error=None)
            log.info("Telegram bot connected as @%s", me.get("username"))
        except Exception as exc:
            message = _redact(exc)
            _status.update(running=False, bot_username=None, error=message)
            log.error("Telegram bot could not authenticate, giving up: %s", message)
            return

        while True:
            try:
                params = {"timeout": _POLL_TIMEOUT}
                if offset is not None:
                    params["offset"] = offset
                updates = await _call(client, "getUpdates", **params) or []
                backoff = 1
                _status["error"] = None

                for update in updates:
                    offset = update["update_id"] + 1
                    await _handle_update(update)

            except asyncio.CancelledError:
                raise
            except Exception as exc:
                message = _redact(exc)
                _status["error"] = message
                log.warning("Telegram polling error (retry in %ss): %s", backoff, message)
                await asyncio.sleep(backoff)
                backoff = min(backoff * 2, 60)


async def start_bot() -> None:
    """Load the config and start polling, if the bot is enabled."""
    global _config, _task

    await stop_bot()
    _config = load_config()

    if not _config.usable:
        _status.update(running=False, bot_username=None, error=None)
        return

    from .imapsync_runner import add_progress_listener
    add_progress_listener(on_progress)

    _task = asyncio.create_task(_run_loop())


async def stop_bot() -> None:
    global _task
    if _task:
        _task.cancel()
        try:
            await _task
        except (asyncio.CancelledError, Exception):
            pass
        _task = None
    _status.update(running=False, bot_username=None)


async def restart_bot() -> None:
    await start_bot()
