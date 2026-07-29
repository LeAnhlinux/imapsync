from datetime import datetime, timedelta

import bcrypt
from cryptography.fernet import Fernet
from jose import jwt

from .config import settings

def _build_fernet() -> Fernet:
    """Fernet cipher for encrypting IMAP passwords at rest.

    Falling back to a generated key would silently make every stored password
    undecryptable on the next restart (and give each uvicorn worker its own
    key), so an unusable key is a startup failure instead.
    """
    key = settings.IMAPSYNC_FERNET_KEY
    try:
        return Fernet(key.encode())
    except Exception as exc:
        raise RuntimeError(
            "IMAPSYNC_FERNET_KEY is missing or invalid. Generate one with:\n"
            "  python3 -c \"from cryptography.fernet import Fernet; "
            "print(Fernet.generate_key().decode())\"\n"
            "and set it in your .env file."
        ) from exc


if settings.SECRET_KEY == "change-me-in-production":
    raise RuntimeError(
        "SECRET_KEY is still the default value. Generate one with: openssl rand -hex 32"
    )

_fernet = _build_fernet()


def hash_password(password: str) -> str:
    return bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()


def verify_password(plain: str, hashed: str) -> bool:
    return bcrypt.checkpw(plain.encode(), hashed.encode())


def create_access_token(data: dict, expires_delta: timedelta | None = None) -> str:
    to_encode = data.copy()
    expire = datetime.utcnow() + (expires_delta or timedelta(minutes=settings.ACCESS_TOKEN_EXPIRE_MINUTES))
    to_encode.update({"exp": expire})
    return jwt.encode(to_encode, settings.SECRET_KEY, algorithm=settings.ALGORITHM)


def decode_access_token(token: str) -> dict | None:
    try:
        return jwt.decode(token, settings.SECRET_KEY, algorithms=[settings.ALGORITHM])
    except Exception:
        return None


def encrypt_password(plain: str) -> str:
    return _fernet.encrypt(plain.encode()).decode()


def decrypt_password(encrypted: str) -> str:
    return _fernet.decrypt(encrypted.encode()).decode()
