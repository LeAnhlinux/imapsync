import asyncio
import os
import re
import signal
import tempfile
from collections import deque
from datetime import datetime
from pathlib import Path
from typing import Callable

from ..config import settings


class RunningJob:
    def __init__(self, job_id: int, process: asyncio.subprocess.Process, logfile: str):
        self.job_id = job_id
        self.process = process
        self.logfile = logfile
        self.log_lines: deque[str] = deque(maxlen=10000)
        self.subscribers: list[Callable] = []
        self.progress: dict = {}

    async def broadcast(self, message: dict):
        for callback in self.subscribers:
            try:
                await callback(message)
            except Exception:
                pass


# Global registry of running jobs
_running_jobs: dict[int, RunningJob] = {}


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
    logfile = str(settings.LOG_DIR / f"{job_id}.txt")

    # Write passwords to temp files (0600 perms)
    passfile1 = tempfile.NamedTemporaryFile(mode='w', prefix='imapsync_p1_', delete=False)
    passfile1.write(password1)
    passfile1.close()
    os.chmod(passfile1.name, 0o600)

    passfile2 = tempfile.NamedTemporaryFile(mode='w', prefix='imapsync_p2_', delete=False)
    passfile2.write(password2)
    passfile2.close()
    os.chmod(passfile2.name, 0o600)

    cmd = [
        settings.IMAPSYNC_PATH,
        '--host1', host1,
        '--user1', user1,
        '--passfile1', passfile1.name,
        '--host2', host2,
        '--user2', user2,
        '--passfile2', passfile2.name,
        '--logfile', logfile,
        '--pidfile', f'/tmp/imapsync_{job_id}.pid',
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

    # Extra options (e.g. --automap, --addheader)
    if extra_options:
        for key, value in extra_options.items():
            if isinstance(value, bool):
                if value:
                    cmd.append(f'--{key}')
            elif value is not None:
                cmd.extend([f'--{key}', str(value)])

    process = await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
    )

    # Clean up passfiles immediately after process starts
    try:
        os.unlink(passfile1.name)
    except OSError:
        pass
    try:
        os.unlink(passfile2.name)
    except OSError:
        pass

    running_job = RunningJob(job_id, process, logfile)
    _running_jobs[job_id] = running_job

    # Start background log reader
    asyncio.create_task(_read_output(running_job, on_complete))

    return running_job


# Regex patterns for parsing imapsync output
_FOLDER_PATTERN = re.compile(r'Syncing folder \[.*?\] \((\d+)/(\d+)\)')
_ETA_PATTERN = re.compile(r'ETA:\s*(\S+)')
_TRANSFER_PATTERN = re.compile(r'(\d+) messages? transferred')
_BYTES_PATTERN = re.compile(r'Transferred:\s+(\d+)')
_ERRORS_PATTERN = re.compile(r'Errors:\s+(\d+)')


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

            eta_match = _ETA_PATTERN.search(line)
            if eta_match:
                running_job.progress["eta"] = eta_match.group(1)

    except Exception:
        pass

    # Wait for process to finish
    await running_job.process.wait()

    exit_code = running_job.process.returncode or 0

    # Parse summary from log
    summary = _parse_summary(running_job.log_lines)

    # Broadcast completion
    status = "completed" if exit_code == 0 else "failed"
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
    """Read the log file for a job."""
    logfile = settings.LOG_DIR / f"{job_id}.txt"
    if logfile.exists():
        return logfile.read_text(errors='replace')

    # Check running job's in-memory buffer
    running_job = _running_jobs.get(job_id)
    if running_job:
        return '\n'.join(running_job.log_lines)

    return None
