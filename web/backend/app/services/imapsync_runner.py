import asyncio
import os
import re
import signal
import tempfile
from collections import deque
from pathlib import Path
from typing import Callable

from ..config import settings
from .imapsync_options import sanitize_extra_options


class RunningJob:
    def __init__(self, job_id: int, process: asyncio.subprocess.Process, logfile: str):
        self.job_id = job_id
        self.process = process
        self.logfile = logfile
        self.log_lines: deque[str] = deque(maxlen=10000)
        self.subscribers: list[Callable] = []
        self.progress: dict = {}
        self.aborted = False

    async def broadcast(self, message: dict):
        for callback in self.subscribers:
            try:
                await callback(message)
            except Exception:
                pass


# Global registry of running jobs
_running_jobs: dict[int, RunningJob] = {}

# Called with (job_id, percent, folder) on every progress update. Listeners
# decide for themselves how often that is worth acting on — the runner does not
# want to know what a "milestone" means to each of them.
_progress_listeners: list[Callable] = []


def add_progress_listener(callback: Callable) -> None:
    if callback not in _progress_listeners:
        _progress_listeners.append(callback)


async def _emit_progress(job_id: int, percent: float, folder: str) -> None:
    for callback in _progress_listeners:
        try:
            await callback(job_id, percent, folder)
        except Exception:
            pass


def get_running_jobs() -> dict[int, RunningJob]:
    return _running_jobs


def get_running_job(job_id: int) -> RunningJob | None:
    return _running_jobs.get(job_id)


async def start_sync(
    job_id: int,
    host1: str,
    user1: str,
    password1: str,
    host2: str,
    user2: str,
    password2: str,
    ssl1: bool = True,
    ssl2: bool = True,
    folders: list[str] | None = None,
    extra_options: dict | None = None,
    dry_run: bool = False,
    on_complete: Callable | None = None,
) -> RunningJob:
    """Start an imapsync subprocess and track it."""
    logfile = str(settings.LOG_DIR.resolve() / f"{job_id}.txt")

    # Write passwords to temp files (0600 perms)
    passfile1 = tempfile.NamedTemporaryFile(mode='w', prefix='imapsync_p1_', delete=False)
    passfile1.write(password1)
    passfile1.close()
    os.chmod(passfile1.name, 0o600)

    passfile2 = tempfile.NamedTemporaryFile(mode='w', prefix='imapsync_p2_', delete=False)
    passfile2.write(password2)
    passfile2.close()
    os.chmod(passfile2.name, 0o600)

    # Ensure tmpdir and cache dir exist with proper permissions
    tmpdir = str(settings.DATA_DIR / "tmp")
    cachedir = str(settings.DATA_DIR / "cache")
    os.makedirs(tmpdir, exist_ok=True)
    os.makedirs(cachedir, exist_ok=True)

    cmd = [
        settings.IMAPSYNC_PATH,
        '--host1', host1,
        '--user1', user1,
        '--passfile1', passfile1.name,
        '--host2', host2,
        '--user2', user2,
        '--passfile2', passfile2.name,
        '--nolog',
        '--tmpdir', tmpdir,
        '--pidfile', f'{tmpdir}/imapsync_{job_id}.pid',
    ]

    if ssl1:
        cmd.append('--ssl1')
    if ssl2:
        cmd.append('--ssl2')
    if dry_run:
        cmd.append('--dry')
    if folders:
        for f in folders:
            cmd.extend(['--folder', f])

    # Extra options (e.g. --automap, --addheader). Anything outside the allowlist
    # is dropped here: these values reach imapsync's argv, which can run shell
    # commands or eval Perl via options like --pipemess or --regexmess.
    safe_options, rejected_options = sanitize_extra_options(extra_options)
    for key, value in safe_options.items():
        if value is True:
            cmd.append(f'--{key}')
        else:
            cmd.extend([f'--{key}', str(value)])

    process = await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
    )

    running_job = RunningJob(job_id, process, logfile)
    running_job._passfiles = [passfile1.name, passfile2.name]  # cleaned after process exits
    if rejected_options:
        running_job.log_lines.append(
            f"Warning: ignored unsupported or unsafe options: {', '.join(sorted(rejected_options))}"
        )
    _running_jobs[job_id] = running_job

    # Start background log reader
    asyncio.create_task(_read_output(running_job, on_complete))

    return running_job


# Regex patterns for parsing imapsync output
_FOLDER_PATTERN = re.compile(r'Syncing folder \[.*?\] \((\d+)/(\d+)\)')
_ETA_PATTERN = re.compile(r'ETA:\s*(\S+)')
_TRANSFER_PATTERN = re.compile(r'Messages transferred\s*:\s*(\d+)')
_BYTES_PATTERN = re.compile(r'Total bytes transferred\s*:\s*(\d+)')
_ERRORS_PATTERN = re.compile(r'Detected\s+(\d+)\s+errors')


async def _read_output(running_job: RunningJob, on_complete: Callable | None):
    """Read subprocess stdout line by line, broadcast to WebSocket subscribers."""
    assert running_job.process.stdout is not None
    try:
        async for raw_line in running_job.process.stdout:
            line = raw_line.decode('utf-8', errors='replace').rstrip('\n')
            running_job.log_lines.append(line)

            # Broadcast log line
            await running_job.broadcast({"type": "log", "data": line})

            # Parse progress
            folder_match = _FOLDER_PATTERN.search(line)
            if folder_match:
                current, total = folder_match.group(1), folder_match.group(2)
                percent = int(current) / int(total) * 100 if int(total) > 0 else 0
                running_job.progress = {
                    "percent": round(percent, 1),
                    "folder": f"{current}/{total}",
                    "eta": running_job.progress.get("eta", ""),
                }
                await running_job.broadcast({"type": "progress", **running_job.progress})
                await _emit_progress(running_job.job_id, percent, running_job.progress["folder"])

            eta_match = _ETA_PATTERN.search(line)
            if eta_match:
                running_job.progress["eta"] = eta_match.group(1)

    except Exception:
        pass

    # Wait for process to finish
    await running_job.process.wait()

    exit_code = running_job.process.returncode or 0

    # Clean up passfiles now that process has exited
    for pf in getattr(running_job, '_passfiles', []):
        try:
            os.unlink(pf)
        except OSError:
            pass

    # Save log to file for later retrieval
    try:
        logpath = Path(running_job.logfile)
        logpath.parent.mkdir(parents=True, exist_ok=True)
        logpath.write_text('\n'.join(running_job.log_lines), encoding='utf-8')
    except Exception:
        pass

    # Parse summary from log
    summary = _parse_summary(running_job.log_lines)

    # Broadcast completion
    if running_job.aborted:
        status = "aborted"
    else:
        status = "completed" if exit_code == 0 else "failed"
    summary["aborted"] = running_job.aborted
    await running_job.broadcast({"type": "status", "status": status, "exit_code": exit_code})

    # Remove from running
    _running_jobs.pop(running_job.job_id, None)

    # Callback for DB update
    if on_complete:
        await on_complete(running_job.job_id, exit_code, summary)


def _parse_summary(log_lines: deque[str]) -> dict:
    """Parse final summary stats from imapsync log output."""
    result = {"messages_transferred": 0, "bytes_transferred": 0, "errors_count": 0, "summary": ""}
    full_text = '\n'.join(log_lines)

    m = _TRANSFER_PATTERN.search(full_text)
    if m:
        result["messages_transferred"] = int(m.group(1))

    m = _BYTES_PATTERN.search(full_text)
    if m:
        result["bytes_transferred"] = int(m.group(1))

    m = _ERRORS_PATTERN.search(full_text)
    if m:
        result["errors_count"] = int(m.group(1))

    # Last 5 lines as summary
    last_lines = list(log_lines)[-5:]
    result["summary"] = '\n'.join(last_lines)

    return result


async def abort_job(job_id: int) -> bool:
    """Abort a running sync job."""
    running_job = _running_jobs.get(job_id)
    if not running_job:
        return False

    running_job.aborted = True
    try:
        running_job.process.send_signal(signal.SIGTERM)
        # Give it 5 seconds to clean up
        try:
            await asyncio.wait_for(running_job.process.wait(), timeout=5)
        except asyncio.TimeoutError:
            running_job.process.kill()
        return True
    except ProcessLookupError:
        return False


def get_job_log(job_id: int) -> str | None:
    """Read log from in-memory buffer (running/finished) or saved log file."""
    # Check running job's in-memory buffer first
    running_job = _running_jobs.get(job_id)
    if running_job:
        return '\n'.join(running_job.log_lines)

    # Check saved log file
    logfile = settings.LOG_DIR.resolve() / f"{job_id}.txt"
    if logfile.exists():
        return logfile.read_text(errors='replace')

    return None
