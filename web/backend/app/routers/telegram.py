from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import User
from ..schemas import TelegramSettingsOut, TelegramSettingsUpdate, TelegramStatus
from ..security import decrypt_password, encrypt_password
from ..services import telegram_bot

router = APIRouter(prefix="/api/telegram", tags=["telegram"])


def _require_admin(user: User) -> None:
    if not user.is_admin:
        raise HTTPException(status_code=403, detail="Admin only")


def _to_out(row) -> TelegramSettingsOut:
    """The token is write-only: it never leaves the server once stored."""
    return TelegramSettingsOut(
        enabled=row.enabled,
        has_token=bool(row.bot_token_enc),
        allowed_chat_ids=row.allowed_chat_ids or "",
        user_id=row.user_id,
        notify_on_success=row.notify_on_success,
        notify_on_failure=row.notify_on_failure,
        notify_on_progress=row.notify_on_progress,
        progress_step=row.progress_step or 25,
    )


@router.get("/settings", response_model=TelegramSettingsOut)
def get_settings(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    _require_admin(user)
    return _to_out(telegram_bot.get_settings_row(db, create=True))


@router.put("/settings", response_model=TelegramSettingsOut)
async def update_settings(
    data: TelegramSettingsUpdate,
    user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _require_admin(user)
    row = telegram_bot.get_settings_row(db, create=True)

    if data.bot_token is not None:
        # An empty string means "forget the token", not "store an empty token".
        row.bot_token_enc = encrypt_password(data.bot_token) if data.bot_token else None
    if data.allowed_chat_ids is not None:
        row.allowed_chat_ids = data.allowed_chat_ids
    if data.user_id is not None:
        if not db.query(User).filter(User.id == data.user_id).first():
            raise HTTPException(status_code=400, detail="No such user")
        row.user_id = data.user_id
    if data.enabled is not None:
        row.enabled = data.enabled
    if data.notify_on_success is not None:
        row.notify_on_success = data.notify_on_success
    if data.notify_on_failure is not None:
        row.notify_on_failure = data.notify_on_failure
    if data.notify_on_progress is not None:
        row.notify_on_progress = data.notify_on_progress
    if data.progress_step is not None:
        if not 1 <= data.progress_step <= 50:
            raise HTTPException(status_code=400, detail="progress_step must be between 1 and 50")
        row.progress_step = data.progress_step

    if row.enabled and not (row.bot_token_enc and row.allowed_chat_ids and row.user_id):
        raise HTTPException(
            status_code=400,
            detail="A bot token, at least one chat id and an app user are all required to enable the bot",
        )

    db.commit()
    db.refresh(row)

    # Pick up the new settings without a service restart.
    await telegram_bot.restart_bot()

    return _to_out(row)


@router.get("/status", response_model=TelegramStatus)
def status(user: User = Depends(get_current_user)):
    _require_admin(user)
    return TelegramStatus(**telegram_bot.get_status())


@router.post("/test")
async def test(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    """Send a test message using the stored settings."""
    _require_admin(user)
    row = telegram_bot.get_settings_row(db)
    if not row or not row.bot_token_enc:
        return {"status": "error", "detail": "No bot token saved yet"}

    chat_ids = telegram_bot.parse_chat_ids(row.allowed_chat_ids)
    if not chat_ids:
        return {"status": "error", "detail": "No chat ids configured"}

    ok, detail = await telegram_bot.send_test_message(decrypt_password(row.bot_token_enc), chat_ids)
    return {"status": "ok" if ok else "error", "detail": detail}
