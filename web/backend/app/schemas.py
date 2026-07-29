from datetime import datetime

from pydantic import BaseModel, EmailStr


# --- Auth ---
class UserRegister(BaseModel):
    username: str
    email: EmailStr
    password: str


class UserLogin(BaseModel):
    username: str
    password: str


class Token(BaseModel):
    access_token: str
    token_type: str = "bearer"


class UserOut(BaseModel):
    id: int
    username: str
    email: str
    is_admin: bool
    created_at: datetime

    class Config:
        from_attributes = True


# --- Profile ---
class ProfileCreate(BaseModel):
    name: str
    host1: str
    user1: str
    password1: str
    ssl1: bool = True
    host2: str
    user2: str
    password2: str
    ssl2: bool = True
    extra_options: dict = {}


class ProfileUpdate(BaseModel):
    name: str | None = None
    host1: str | None = None
    user1: str | None = None
    password1: str | None = None
    ssl1: bool | None = None
    host2: str | None = None
    user2: str | None = None
    password2: str | None = None
    ssl2: bool | None = None
    extra_options: dict | None = None


class ProfileOut(BaseModel):
    id: int
    name: str
    host1: str
    user1: str
    ssl1: bool
    host2: str
    user2: str
    ssl2: bool
    extra_options: dict
    created_at: datetime
    updated_at: datetime

    class Config:
        from_attributes = True


# --- Sync ---
class SyncStart(BaseModel):
    profile_id: int | None = None
    # Direct params (used if no profile_id)
    host1: str | None = None
    user1: str | None = None
    password1: str | None = None
    ssl1: bool = True
    host2: str | None = None
    user2: str | None = None
    password2: str | None = None
    ssl2: bool = True
    extra_options: dict = {}
    folders: list[str] = []
    dry_run: bool = False


class SyncJobOut(BaseModel):
    id: int
    user_id: int
    profile_id: int | None
    source_account: str | None = None
    dest_account: str | None = None
    status: str
    pid: int | None
    started_at: datetime | None
    finished_at: datetime | None
    exit_code: int | None
    messages_transferred: int
    bytes_transferred: int
    errors_count: int
    summary: str | None

    class Config:
        from_attributes = True


# --- Schedule ---
class ScheduleCreate(BaseModel):
    profile_id: int
    cron_expression: str
    is_active: bool = True


class ScheduleUpdate(BaseModel):
    cron_expression: str | None = None
    is_active: bool | None = None


class ScheduleOut(BaseModel):
    id: int
    profile_id: int
    cron_expression: str
    is_active: bool
    last_run_at: datetime | None
    next_run_at: datetime | None
    created_at: datetime

    class Config:
        from_attributes = True


# --- Batch ---
class BatchRunOut(BaseModel):
    id: int
    filename: str
    total_rows: int
    completed_rows: int
    failed_rows: int
    status: str
    created_at: datetime

    class Config:
        from_attributes = True


# --- Notifications ---
class NotificationSettingsUpdate(BaseModel):
    smtp_host: str | None = None
    smtp_port: int = 587
    smtp_user: str | None = None
    smtp_password: str | None = None
    from_email: str | None = None
    to_email: str | None = None
    notify_on_success: bool = False
    notify_on_failure: bool = True


class NotificationSettingsOut(BaseModel):
    smtp_host: str | None
    smtp_port: int
    smtp_user: str | None
    from_email: str | None
    to_email: str | None
    notify_on_success: bool
    notify_on_failure: bool

    class Config:
        from_attributes = True


# --- History ---
class HistoryStats(BaseModel):
    total_syncs: int
    successful: int
    failed: int
    total_messages: int
    total_bytes: int
    success_rate: float


# --- Folders ---
class FolderBrowseRequest(BaseModel):
    host: str
    user: str
    password: str
    ssl: bool = True


class FolderInfo(BaseModel):
    name: str
    messages: int = 0
    size: int = 0
