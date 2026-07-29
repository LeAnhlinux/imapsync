"""Parse the /run command's mailbox specification.

Six semicolon-separated fields in a row read fine to a machine and badly to a
person: nothing in `a;b;c;d;e;f` says which half is the source and which is the
destination, and getting that backwards copies the wrong way. So the documented
form labels the two sides:

    /run
    from: mail.old.com;me@old.com;password
    to:   mail.new.com;me@new.com;password
    options: --dry --maxage 30

The flat positional form is still accepted, because it is what the CSV batch
importer uses and pasting one line out of a CSV should work.
"""

import re
from dataclasses import dataclass, field

from .csv_parser import _parse_extra_options, parse_csv

# Aliases so nobody has to remember which word we picked, including the
# Vietnamese ones this is most often typed in.
_FROM_LABELS = {"from", "source", "src", "old", "cu", "cũ", "nguon", "nguồn", "1"}
_TO_LABELS = {"to", "dest", "destination", "target", "new", "moi", "mới", "dich", "đích", "2"}
_OPTION_LABELS = {"options", "option", "opts", "flags", "tuychon"}

# A label is a bare word before a colon. Requiring "no dots" keeps host:port
# and bare hostnames in the flat form from looking like labels.
_LABEL_LINE = re.compile(r"^\s*([^\s:;.]+)\s*:")

USAGE = """Usage:

/run
from: mail.old.com;me@old.com;password
to: mail.new.com;me@new.com;password

Each side is host;user;password — 'from' is the mailbox being copied,
'to' is where it lands.

Optional third line:
options: --dry --maxage 30 --nossl2

(The flat form host1;user1;pass1;host2;user2;pass2 also works.)"""


@dataclass
class RunSpec:
    host1: str
    user1: str
    password1: str
    host2: str
    user2: str
    password2: str
    extra_options: dict = field(default_factory=dict)


class RunSyntaxError(ValueError):
    """Raised with a message meant to be shown to the user verbatim."""


def _split_side(label: str, value: str) -> tuple[str, str, str]:
    parts = [p.strip() for p in value.split(";")]
    if len(parts) != 3 or not all(parts):
        raise RunSyntaxError(
            f"The '{label}' line needs exactly host;user;password — got {len(parts)} field(s).\n\n{USAGE}"
        )
    return parts[0], parts[1], parts[2]


def is_labeled(text: str) -> bool:
    """True when the message uses the labelled form rather than the flat one.

    Any bare word before a colon counts, not just the ones we know: a typo like
    "fromm:" should get "Unknown label" rather than falling through to the flat
    parser and failing as if the whole thing were unreadable.
    """
    return any(_LABEL_LINE.match(line) for line in text.splitlines() if line.strip())


def parse_labeled(text: str) -> RunSpec:
    source = None
    dest = None
    options: dict = {}

    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue

        match = _LABEL_LINE.match(line)
        if not match:
            raise RunSyntaxError(f"I do not understand this line:\n  {line}\n\n{USAGE}")

        key = match.group(1).lower()
        value = line[match.end():].strip()

        if key in _FROM_LABELS:
            source = _split_side(key, value)
        elif key in _TO_LABELS:
            dest = _split_side(key, value)
        elif key in _OPTION_LABELS:
            options = _parse_extra_options(value)
        else:
            raise RunSyntaxError(f"Unknown label '{match.group(1)}'.\n\n{USAGE}")

    if source is None and dest is None:
        raise RunSyntaxError(USAGE)
    if source is None:
        raise RunSyntaxError(f"Missing the 'from' line — I do not know what to copy.\n\n{USAGE}")
    if dest is None:
        raise RunSyntaxError(f"Missing the 'to' line — I do not know where to copy it.\n\n{USAGE}")

    return RunSpec(
        host1=source[0], user1=source[1], password1=source[2],
        host2=dest[0], user2=dest[1], password2=dest[2],
        extra_options=options,
    )


def parse_flat(text: str) -> list[RunSpec]:
    """The positional host1;user1;pass1;host2;user2;pass2 form, one per line."""
    rows = parse_csv(text)
    return [
        RunSpec(
            host1=r["host1"], user1=r["user1"], password1=r["password1"],
            host2=r["host2"], user2=r["user2"], password2=r["password2"],
            extra_options=r.get("extra_options") or {},
        )
        for r in rows
    ]


def describe(spec: RunSpec, dry_run: bool = False) -> str:
    """Spell out the direction so a mistake is obvious before it copies."""
    lines = [
        f"  FROM  {spec.user1}  on {spec.host1}",
        f"  TO    {spec.user2}  on {spec.host2}",
    ]
    if dry_run:
        lines.append("  MODE  dry run, nothing will be written")
    return "\n".join(lines)
