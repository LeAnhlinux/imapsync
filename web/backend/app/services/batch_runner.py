"""Create and run batch imports.

Lives here rather than in the router because the Telegram bot needs the same
two operations and a CSV uploaded through chat should behave identically to one
uploaded through the web form.
"""

from sqlalchemy.orm import Session

from ..database import SessionLocal
from ..models import BatchRun, SyncJob, now_vn
from ..security import decrypt_password, encrypt_password
from .csv_parser import parse_csv
from .imapsync_options import sanitize_extra_options
from .imapsync_runner import start_sync
from .job_completion import finalize_job

DEFAULT_OPTIONS = {
    "dry_run": False,
    "ssl1": True,
    "ssl2": True,
    "automap": True,
    "usecache": True,
}


def create_batch(
    db: Session,
    user_id: int,
    text: str,
    filename: str,
    options: dict | None = None,
) -> BatchRun:
    """Parse CSV text into a pending BatchRun. Raises ValueError if empty."""
    rows = parse_csv(text)
    if not rows:
        raise ValueError("No valid rows found in CSV data")

    # Tell the user which options were thrown out instead of dropping them
    # silently further down in the runner.
    for line_no, row in enumerate(rows, 1):
        _, rejected = sanitize_extra_options(row.get("extra_options"))
        if rejected:
            raise ValueError(
                f"Row {line_no}: unsupported or unsafe imapsync options: {', '.join(sorted(rejected))}"
            )

    # batch_data lands in the database, so the IMAP passwords get the same
    # Fernet treatment as the ones stored on a Profile.
    for row in rows:
        row["password1"] = encrypt_password(row["password1"])
        row["password2"] = encrypt_password(row["password2"])

    opts = {**DEFAULT_OPTIONS, **(options or {})}

    batch = BatchRun(
        user_id=user_id,
        filename=filename,
        total_rows=len(rows),
        status="pending",
        batch_data={"rows": rows, **opts},
    )
    db.add(batch)
    db.commit()
    db.refresh(batch)
    return batch


def describe_batch(batch: BatchRun) -> str:
    """Human-readable preview. Never includes passwords."""
    data = batch.batch_data or {}
    rows = data.get("rows", [])
    lines = [f"Batch #{batch.id} from {batch.filename}: {len(rows)} row(s)"]
    for i, row in enumerate(rows[:10], 1):
        lines.append(f"  {i}. {row['user1']} on {row['host1']} -> {row['user2']} on {row['host2']}")
    if len(rows) > 10:
        lines.append(f"  ... and {len(rows) - 10} more")
    flags = [k for k in ("dry_run", "automap", "usecache") if data.get(k)]
    lines.append(f"Options: {', '.join(flags) if flags else 'none'}")
    return "\n".join(lines)


async def run_batch(batch_id: int, user_id: int, data: dict) -> None:
    """Run every row in the batch, one after another."""
    rows = data["rows"]
    dry_run = data.get("dry_run", False)
    default_ssl1 = data.get("ssl1", True)
    default_ssl2 = data.get("ssl2", True)
    automap = data.get("automap", True)
    usecache = data.get("usecache", True)

    session = SessionLocal()
    try:
        batch = session.query(BatchRun).filter(BatchRun.id == batch_id).first()
        if not batch:
            return

        for row in rows:
            job = SyncJob(
                user_id=user_id,
                source_account=f"{row['user1']}@{row['host1']}",
                dest_account=f"{row['user2']}@{row['host2']}",
                status="running",
                started_at=now_vn(),
            )
            session.add(job)
            session.commit()
            session.refresh(job)

            job.logfile = f"data/logs/{job.id}.txt"
            session.commit()

            try:
                # Merge global options with per-row extra_options
                extra = dict(row.get("extra_options") or {})
                if automap:
                    extra["automap"] = True
                if usecache:
                    extra["usecache"] = True

                running_job = await start_sync(
                    job_id=job.id,
                    host1=row["host1"], user1=row["user1"],
                    password1=decrypt_password(row["password1"]),
                    host2=row["host2"], user2=row["user2"],
                    password2=decrypt_password(row["password2"]),
                    ssl1=row.get("ssl1", default_ssl1),
                    ssl2=row.get("ssl2", default_ssl2),
                    extra_options=extra,
                    dry_run=dry_run,
                    on_complete=finalize_job,
                )
                # Wait for it to complete (sequential)
                await running_job.process.wait()
                batch.completed_rows += 1
            except Exception:
                batch.failed_rows += 1

            session.commit()

        batch.status = "completed"
        # Clear batch_data after completion: it holds credentials.
        batch.batch_data = None
        session.commit()
    finally:
        session.close()
