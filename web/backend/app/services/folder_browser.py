import asyncio
import os
import re
import tempfile

from ..config import settings
from ..schemas import FolderInfo


async def browse_folders(host: str, user: str, password: str, ssl: bool = True) -> list[FolderInfo]:
    """Run imapsync --justfoldersizes to list folders and their sizes."""
    passfile = tempfile.NamedTemporaryFile(mode='w', prefix='imapsync_browse_', delete=False)
    passfile.write(password)
    passfile.close()
    os.chmod(passfile.name, 0o600)

    cmd = [
        settings.IMAPSYNC_PATH,
        '--host1', host,
        '--user1', user,
        '--passfile1', passfile.name,
        '--host2', host,  # dummy, required by imapsync
        '--user2', user,
        '--passfile2', passfile.name,
        '--justfoldersizes',
        '--dry',
        '--nolog',
    ]
    if ssl:
        cmd.extend(['--ssl1', '--ssl2'])

    process = await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
    )

    try:
        stdout, _ = await asyncio.wait_for(process.communicate(), timeout=60)
    except asyncio.TimeoutError:
        process.kill()
        await process.wait()
        raise
    finally:
        # Only now: imapsync reads the passfile after it starts, so unlinking
        # right after the spawn races the child and makes the login fail.
        try:
            os.unlink(passfile.name)
        except OSError:
            pass

    output = stdout.decode('utf-8', errors='replace')

    return _parse_folder_sizes(output)


# Pattern: Stripping IMAP folder listing
# Example: "Stripping leading strp Strp" ... "Strp Strp ..."
# Or folder listing:  Stripping  leading  string  from  folder  names  or similar
_FOLDER_LINE = re.compile(
    r'^\s*Stripping\s|'
    r'^\s*Strp\s|'
    r'^\s*Stripping\s'
)

# Folder size line: "Strp Strp INBOX     has   123 messages  456789 bytes"
# Or: "Strp Strp [INBOX]   123 messages  456789 bytes"
_SIZE_PATTERN = re.compile(
    r'^\s*(?:Strp\s+)*\[?([^\]]+)\]?\s+(?:has\s+)?(\d+)\s+messages?\s+(\d+)\s+bytes?',
    re.MULTILINE
)

# More generic: folder lines from --justfoldersizes output
_FOLDER_PATTERN = re.compile(
    r'Strp\s+Strp\s+(\S+)\s+has\s+(\d+)\s+messages?\s+(\d+)\s+bytes?'
)


def _parse_folder_sizes(output: str) -> list[FolderInfo]:
    """Parse imapsync --justfoldersizes output into folder list."""
    folders = []
    seen = set()

    # Try multiple patterns
    for pattern in [_FOLDER_PATTERN, _SIZE_PATTERN]:
        for match in pattern.finditer(output):
            name = match.group(1).strip()
            messages = int(match.group(2))
            size = int(match.group(3))
            if name not in seen:
                seen.add(name)
                folders.append(FolderInfo(name=name, messages=messages, size=size))

    # Fallback: parse "Strp" lines more loosely
    if not folders:
        for line in output.splitlines():
            if 'messages' in line and 'bytes' in line:
                parts = re.search(r'(\S+)\s+(?:has\s+)?(\d+)\s+messages?\s+(\d+)\s+bytes?', line)
                if parts:
                    name = parts.group(1).strip('[]')
                    if name not in seen:
                        seen.add(name)
                        folders.append(FolderInfo(
                            name=name,
                            messages=int(parts.group(2)),
                            size=int(parts.group(3)),
                        ))

    return folders
