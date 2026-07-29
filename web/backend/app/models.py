from datetime import datetime, timezone, timedelta

TZ_VN = timezone(timedelta(hours=7))


def now_vn():
    return datetime.now(TZ_VN).replace(tzinfo=None)

from sqlalchemy import Boolean, Column, DateTime, ForeignKey, Integer, String, Text, JSON
from sqlalchemy.orm import relationship

from .database import Base


class User(Base):
    __tablename__ = "users"

    id = Column(Integer, primary_key=True, index=True)
    username = Column(String(50), unique=True, index=True, nullable=False)
    email = Column(String(255), unique=True, index=True, nullable=False)
    hashed_password = Column(String(255), nullable=False)
    is_admin = Column(Boolean, default=False)
    created_at = Column(DateTime, default=now_vn)

    profiles = relationship("Profile", back_populates="user", cascade="all, delete-orphan")
    sync_jobs = relationship("SyncJob", back_populates="user", cascade="all, delete-orphan")
    schedules = relationship("Schedule", back_populates="user", cascade="all, delete-orphan")
    batch_runs = relationship("BatchRun", back_populates="user", cascade="all, delete-orphan")
    notification_settings = relationship("NotificationSettings", back_populates="user", uselist=False, cascade="all, delete-orphan")


class Profile(Base):
    __tablename__ = "profiles"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    name = Column(String(100), nullable=False)
    host1 = Column(String(255), nullable=False)
    user1 = Column(String(255), nullable=False)
    password1_enc = Column(Text, nullable=False)
    ssl1 = Column(Boolean, default=True)
    host2 = Column(String(255), nullable=False)
    user2 = Column(String(255), nullable=False)
    password2_enc = Column(Text, nullable=False)
    ssl2 = Column(Boolean, default=True)
    extra_options = Column(JSON, default=dict)
    created_at = Column(DateTime, default=now_vn)
    updated_at = Column(DateTime, default=now_vn, onupdate=now_vn)

    user = relationship("User", back_populates="profiles")
    sync_jobs = relationship("SyncJob", back_populates="profile")
    schedules = relationship("Schedule", back_populates="profile")


class SyncJob(Base):
    __tablename__ = "sync_jobs"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    profile_id = Column(Integer, ForeignKey("profiles.id"), nullable=True)
    source_account = Column(String(255), nullable=True)  # user1@host1
    dest_account = Column(String(255), nullable=True)    # user2@host2
    status = Column(String(20), default="pending", index=True)  # pending/running/completed/failed/aborted
    pid = Column(Integer, nullable=True)
    logfile = Column(String(500), nullable=True)
    started_at = Column(DateTime, nullable=True)
    finished_at = Column(DateTime, nullable=True)
    exit_code = Column(Integer, nullable=True)
    messages_transferred = Column(Integer, default=0)
    bytes_transferred = Column(Integer, default=0)
    errors_count = Column(Integer, default=0)
    summary = Column(Text, nullable=True)
    # Store sync params for jobs started without a profile
    sync_params = Column(JSON, nullable=True)

    user = relationship("User", back_populates="sync_jobs")
    profile = relationship("Profile", back_populates="sync_jobs")


class Schedule(Base):
    __tablename__ = "schedules"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    profile_id = Column(Integer, ForeignKey("profiles.id"), nullable=False)
    cron_expression = Column(String(100), nullable=False)
    is_active = Column(Boolean, default=True)
    last_run_at = Column(DateTime, nullable=True)
    next_run_at = Column(DateTime, nullable=True)
    created_at = Column(DateTime, default=now_vn)

    user = relationship("User", back_populates="schedules")
    profile = relationship("Profile", back_populates="schedules")


class BatchRun(Base):
    __tablename__ = "batch_runs"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    filename = Column(String(255), nullable=False)
    total_rows = Column(Integer, default=0)
    completed_rows = Column(Integer, default=0)
    failed_rows = Column(Integer, default=0)
    status = Column(String(20), default="pending")  # pending/running/completed/failed
    batch_data = Column(JSON, nullable=True)  # stores parsed rows + options
    created_at = Column(DateTime, default=now_vn)

    user = relationship("User", back_populates="batch_runs")


class TelegramSettings(Base):
    """Single-row table (id=1): one bot per installation.

    The bot acts as one app user, so a per-user table would imply one polling
    loop per user — more machinery than an admin tool needs.
    """

    __tablename__ = "telegram_settings"

    id = Column(Integer, primary_key=True, index=True)
    enabled = Column(Boolean, default=False)
    bot_token_enc = Column(Text, nullable=True)
    # Comma-separated chat ids. This list is the bot's entire access control.
    allowed_chat_ids = Column(String(500), nullable=True)
    # The app user whose profiles and jobs the bot operates on.
    user_id = Column(Integer, ForeignKey("users.id"), nullable=True)
    notify_on_success = Column(Boolean, default=True)
    notify_on_failure = Column(Boolean, default=True)
    notify_on_progress = Column(Boolean, default=True)
    progress_step = Column(Integer, default=25)  # percent between updates
    updated_at = Column(DateTime, default=now_vn, onupdate=now_vn)


class NotificationSettings(Base):
    __tablename__ = "notification_settings"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id"), unique=True, nullable=False)
    smtp_host = Column(String(255), nullable=True)
    smtp_port = Column(Integer, default=587)
    smtp_user = Column(String(255), nullable=True)
    smtp_password_enc = Column(Text, nullable=True)
    from_email = Column(String(255), nullable=True)
    to_email = Column(String(255), nullable=True)
    notify_on_success = Column(Boolean, default=False)
    notify_on_failure = Column(Boolean, default=True)

    user = relationship("User", back_populates="notification_settings")
