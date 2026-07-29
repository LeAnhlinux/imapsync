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
