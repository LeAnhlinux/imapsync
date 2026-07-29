from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import Profile, User
from ..schemas import ProfileCreate, ProfileOut, ProfileUpdate
from ..security import encrypt_password
from ..services.imapsync_options import sanitize_extra_options

router = APIRouter(prefix="/api/profiles", tags=["profiles"])


def _validate_extra_options(extra: dict | None) -> dict:
    """Reject unsupported options at the API boundary so the user sees why."""
    accepted, rejected = sanitize_extra_options(extra)
    if rejected:
        raise HTTPException(
            status_code=400,
            detail=f"Unsupported or unsafe imapsync options: {', '.join(sorted(rejected))}",
        )
    return accepted


@router.get("", response_model=list[ProfileOut])
def list_profiles(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    return db.query(Profile).filter(Profile.user_id == user.id).all()


@router.post("", response_model=ProfileOut, status_code=201)
def create_profile(data: ProfileCreate, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    profile = Profile(
        user_id=user.id,
        name=data.name,
        host1=data.host1, user1=data.user1,
        password1_enc=encrypt_password(data.password1),
        ssl1=data.ssl1,
        host2=data.host2, user2=data.user2,
        password2_enc=encrypt_password(data.password2),
        ssl2=data.ssl2,
        extra_options=_validate_extra_options(data.extra_options),
    )
    db.add(profile)
    db.commit()
    db.refresh(profile)
    return profile


@router.get("/{profile_id}", response_model=ProfileOut)
def get_profile(profile_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    profile = db.query(Profile).filter(Profile.id == profile_id, Profile.user_id == user.id).first()
    if not profile:
        raise HTTPException(status_code=404, detail="Profile not found")
    return profile


@router.put("/{profile_id}", response_model=ProfileOut)
def update_profile(profile_id: int, data: ProfileUpdate, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    profile = db.query(Profile).filter(Profile.id == profile_id, Profile.user_id == user.id).first()
    if not profile:
        raise HTTPException(status_code=404, detail="Profile not found")

    if data.name is not None: profile.name = data.name
    if data.host1 is not None: profile.host1 = data.host1
    if data.user1 is not None: profile.user1 = data.user1
    if data.password1 is not None: profile.password1_enc = encrypt_password(data.password1)
    if data.ssl1 is not None: profile.ssl1 = data.ssl1
    if data.host2 is not None: profile.host2 = data.host2
    if data.user2 is not None: profile.user2 = data.user2
    if data.password2 is not None: profile.password2_enc = encrypt_password(data.password2)
    if data.ssl2 is not None: profile.ssl2 = data.ssl2
    if data.extra_options is not None: profile.extra_options = _validate_extra_options(data.extra_options)

    db.commit()
    db.refresh(profile)
    return profile


@router.delete("/{profile_id}")
def delete_profile(profile_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    profile = db.query(Profile).filter(Profile.id == profile_id, Profile.user_id == user.id).first()
    if not profile:
        raise HTTPException(status_code=404, detail="Profile not found")
    db.delete(profile)
    db.commit()
    return {"status": "deleted"}
