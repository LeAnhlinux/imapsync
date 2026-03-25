from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import NotificationSettings, User
from ..schemas import NotificationSettingsOut, NotificationSettingsUpdate
from ..security import encrypt_password
from ..services.notifier import send_test_email

router = APIRouter(prefix="/api/notifications", tags=["notifications"])


@router.get("/settings", response_model=NotificationSettingsOut | None)
def get_settings(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    settings = db.query(NotificationSettings).filter(NotificationSettings.user_id == user.id).first()
    return settings


@router.put("/settings", response_model=NotificationSettingsOut)
def update_settings(data: NotificationSettingsUpdate, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    settings = db.query(NotificationSettings).filter(NotificationSettings.user_id == user.id).first()
    if not settings:
        settings = NotificationSettings(user_id=user.id)
        db.add(settings)

    if data.smtp_host is not None: settings.smtp_host = data.smtp_host
    if data.smtp_port is not None: settings.smtp_port = data.smtp_port
    if data.smtp_user is not None: settings.smtp_user = data.smtp_user
    if data.smtp_password is not None: settings.smtp_password_enc = encrypt_password(data.smtp_password)
    if data.from_email is not None: settings.from_email = data.from_email
    if data.to_email is not None: settings.to_email = data.to_email
    settings.notify_on_success = data.notify_on_success
    settings.notify_on_failure = data.notify_on_failure

    db.commit()
    db.refresh(settings)
    return settings


@router.post("/test")
async def test(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    settings = db.query(NotificationSettings).filter(NotificationSettings.user_id == user.id).first()
    if not settings or not settings.smtp_host:
        return {"status": "error", "detail": "SMTP not configured"}

    from ..security import decrypt_password
    success = await send_test_email(
        smtp_host=settings.smtp_host,
        smtp_port=settings.smtp_port,
        smtp_user=settings.smtp_user,
        smtp_password=decrypt_password(settings.smtp_password_enc) if settings.smtp_password_enc else None,
        from_email=settings.from_email or "",
        to_email=settings.to_email or "",
    )
    return {"status": "ok" if success else "error"}
