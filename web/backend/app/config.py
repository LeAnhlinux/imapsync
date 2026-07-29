from pathlib import Path
from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    SECRET_KEY: str = "change-me-in-production"
    IMAPSYNC_FERNET_KEY: str = "change-me-in-production"
    DATABASE_URL: str = "sqlite:///./data/imapsync.db"
    IMAPSYNC_PATH: str = "/usr/local/bin/imapsync"
    DOMAIN: str = "localhost"

    # JWT
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 60 * 24  # 24 hours
    ALGORITHM: str = "HS256"

    # Data directories (resolved to absolute paths)
    DATA_DIR: Path = Path("./data")
    LOG_DIR: Path = Path("./data/logs")

    # Telegram bot (disabled unless all three are set)
    TELEGRAM_BOT_TOKEN: str = ""
    # Comma-separated chat ids allowed to talk to the bot. Everything else is
    # ignored: the bot can start syncs and read logs, so this is the only gate.
    TELEGRAM_ALLOWED_CHAT_IDS: str = ""
    # Username of the account the bot acts as — jobs and profiles belong to it.
    TELEGRAM_USERNAME: str = ""

    @property
    def telegram_chat_ids(self) -> set[int]:
        ids = set()
        for part in self.TELEGRAM_ALLOWED_CHAT_IDS.split(","):
            part = part.strip()
            if part:
                try:
                    ids.add(int(part))
                except ValueError:
                    continue
        return ids

    @property
    def telegram_enabled(self) -> bool:
        return bool(self.TELEGRAM_BOT_TOKEN and self.TELEGRAM_USERNAME and self.telegram_chat_ids)

    def model_post_init(self, __context):
        # Ensure paths are absolute
        object.__setattr__(self, 'DATA_DIR', self.DATA_DIR.resolve())
        object.__setattr__(self, 'LOG_DIR', self.LOG_DIR.resolve())

    class Config:
        env_file = ".env"
        env_file_encoding = "utf-8"


settings = Settings()

# Ensure data directories exist
settings.DATA_DIR.mkdir(parents=True, exist_ok=True)
settings.LOG_DIR.mkdir(parents=True, exist_ok=True)
