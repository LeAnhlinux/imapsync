import time
from collections import defaultdict, deque

from fastapi import APIRouter, Depends, HTTPException, Request, status
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import User
from ..schemas import Token, UserLogin, UserOut, UserRegister
from ..security import create_access_token, verify_password

router = APIRouter(prefix="/api/auth", tags=["auth"])

# Sliding-window throttle for failed logins. In-process state, which is fine
# because the service runs a single uvicorn worker (see imapsync-web.service).
_MAX_ATTEMPTS = 10
_WINDOW_SECONDS = 300
_failed_attempts: dict[str, deque[float]] = defaultdict(deque)


def _throttle_key(request: Request, username: str) -> str:
    client = request.client.host if request.client else "unknown"
    return f"{client}:{username}"


def _check_throttle(key: str) -> None:
    attempts = _failed_attempts[key]
    cutoff = time.monotonic() - _WINDOW_SECONDS
    while attempts and attempts[0] < cutoff:
        attempts.popleft()
    if len(attempts) >= _MAX_ATTEMPTS:
        raise HTTPException(
            status_code=status.HTTP_429_TOO_MANY_REQUESTS,
            detail="Too many failed login attempts. Try again in a few minutes.",
        )


@router.post("/register", response_model=UserOut, status_code=status.HTTP_201_CREATED)
def register(data: UserRegister, db: Session = Depends(get_db)):
    # Registration is disabled — admin account is created via deploy script
    raise HTTPException(status_code=403, detail="Registration is disabled")


@router.post("/login", response_model=Token)
def login(data: UserLogin, request: Request, db: Session = Depends(get_db)):
    key = _throttle_key(request, data.username)
    _check_throttle(key)

    user = db.query(User).filter(User.username == data.username).first()
    if not user or not verify_password(data.password, user.hashed_password):
        _failed_attempts[key].append(time.monotonic())
        raise HTTPException(status_code=401, detail="Invalid credentials")

    _failed_attempts.pop(key, None)
    token = create_access_token({"sub": str(user.id)})
    return Token(access_token=token)


@router.get("/me", response_model=UserOut)
def me(user: User = Depends(get_current_user)):
    return user
