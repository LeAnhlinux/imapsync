from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from ..deps import get_current_user, get_db
from ..models import Profile, Schedule, User
from ..schemas import ScheduleCreate, ScheduleOut, ScheduleUpdate

router = APIRouter(prefix="/api/schedules", tags=["schedules"])


@router.get("", response_model=list[ScheduleOut])
def list_schedules(user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    return db.query(Schedule).filter(Schedule.user_id == user.id).all()


@router.post("", response_model=ScheduleOut, status_code=201)
def create_schedule(data: ScheduleCreate, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    # Verify profile belongs to user
    profile = db.query(Profile).filter(Profile.id == data.profile_id, Profile.user_id == user.id).first()
    if not profile:
        raise HTTPException(status_code=404, detail="Profile not found")

    schedule = Schedule(
        user_id=user.id,
        profile_id=data.profile_id,
        cron_expression=data.cron_expression,
        is_active=data.is_active,
    )
    db.add(schedule)
    db.commit()
    db.refresh(schedule)

    # Register with APScheduler
    from ..services.scheduler import add_schedule_job
    add_schedule_job(schedule)

    return schedule


@router.put("/{schedule_id}", response_model=ScheduleOut)
def update_schedule(schedule_id: int, data: ScheduleUpdate, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    schedule = db.query(Schedule).filter(Schedule.id == schedule_id, Schedule.user_id == user.id).first()
    if not schedule:
        raise HTTPException(status_code=404, detail="Schedule not found")

    if data.cron_expression is not None:
        schedule.cron_expression = data.cron_expression
    if data.is_active is not None:
        schedule.is_active = data.is_active

    db.commit()
    db.refresh(schedule)

    # Update APScheduler
    from ..services.scheduler import update_schedule_job
    update_schedule_job(schedule)

    return schedule


@router.delete("/{schedule_id}")
def delete_schedule(schedule_id: int, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    schedule = db.query(Schedule).filter(Schedule.id == schedule_id, Schedule.user_id == user.id).first()
    if not schedule:
        raise HTTPException(status_code=404, detail="Schedule not found")

    # Remove from APScheduler
    from ..services.scheduler import remove_schedule_job
    remove_schedule_job(schedule.id)

    db.delete(schedule)
    db.commit()
    return {"status": "deleted"}
