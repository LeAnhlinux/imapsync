"""Parse the /run command's mailbox specification.

Six semicolon-separated fields in a row read fine to a machine and badly to a
person: nothing in `a;b;c;d;e;f` says where the source ends and the destination
begins, and getting that backwards copies the wrong way. A space splits the two
sides, semicolons split the fields inside one side:

    /run mail.old.com;me@old.com;password mail.new.com;me@new.com;password

Options go at the end, and a password containing a space needs the labelled
form below instead, since there is nothing else to split on.
"""

import re
from dataclasses import dataclass, field

from .csv_parser import _parse_extra_options

# Aliases so nobody has to remember which word we picked, including the
# Vietnamese ones this is most often typed in.
_FROM_LABELS = {"from", "source", "src", "old", "cu", "cũ", "nguon", "nguồn", "1"}
_TO_LABELS = {"to", "dest", "destination", "target", "new", "moi", "mới", "dich", "đích", "2"}
_OPTION_LABELS = {"options", "option", "opts", "flags", "tuychon"}

# A label is a bare word before a colon. Requiring "no dots" keeps host:port
# and bare hostnames from looking like labels.
_LABEL_LINE = re.compile(r"^\s*([^\s:;.]+)\s*:")

USAGE = """Usage:

/run mail.old.com;me@old.com;password mail.new.com;me@new.com;password

A space separates the two mailboxes; each one is host;user;password.
The left one is copied to the right one.

Options go at the end:
/run old.com;me;pw new.com;me;pw --dry --maxage 30 --nossl2

Paste one such line per mailbox to queue several as a batch.

If a password contains a space, use the long form instead:
/run
from: mail.old.com;me@old.com;my password
to: mail.new.com;me@new.com;password"""


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


def _split_side(what: str, value: str) -> tuple[str, str, str]:
    parts = [p.strip() for p in value.split(";")]
    if len(parts) != 3 or not all(parts):
        raise RunSyntaxError(
            f"The {what} mailbox needs exactly host;user;password — "
            f"got {len(parts)} field(s) in '{value}'.\n\n{USAGE}"
        )
    return parts[0], parts[1], parts[2]


def is_labeled(text: str) -> bool:
    """True when the message uses the from:/to: form rather than the short one.

    Any bare word before a colon counts, not just the ones we know: a typo like
    "fromm:" should get "Unknown label" rather than falling through to the
    positional parser and failing as if the whole thing were unreadable.
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
            source = _split_side("'from'", value)
        elif key in _TO_LABELS:
            dest = _split_side("'to'", value)
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


def parse_line(line: str) -> RunSpec:
    """One mailbox pair: `host;user;pw host;user;pw [--options]`."""
    tokens = line.split()
    if not tokens:
        raise RunSyntaxError(USAGE)

    # Options run from the first --flag to the end of the line, so a flag that
    # takes a value ("--maxage 30") keeps its value.
    opt_start = next((i for i, t in enumerate(tokens) if t.startswith("--")), len(tokens))
    sides = tokens[:opt_start]
    options = _parse_extra_options(" ".join(tokens[opt_start:]))

    if len(sides) == 2:
        host1, user1, password1 = _split_side("source", sides[0])
        host2, user2, password2 = _split_side("destination", sides[1])
    elif len(sides) == 1:
        # The flat CSV row, so a line copied out of a batch file still works.
        fields = [f.strip() for f in sides[0].split(";")]
        if len(fields) < 6 or not all(fields[:6]):
            raise RunSyntaxError(
                "I need two mailboxes separated by a space.\n\n" + USAGE
            )
        host1, user1, password1, host2, user2, password2 = fields[:6]
        if len(fields) > 6 and fields[6]:
            options = {**_parse_extra_options(fields[6]), **options}
    else:
        raise RunSyntaxError(
            f"I found {len(sides)} mailboxes on that line, I need exactly 2. "
            "A password with a space in it needs the long form.\n\n" + USAGE
        )

    return RunSpec(
        host1=host1, user1=user1, password1=password1,
        host2=host2, user2=user2, password2=password2,
        extra_options=options,
    )


def parse_run(text: str) -> list[RunSpec]:
    """Parse a whole /run body: labelled block, or one mailbox pair per line."""
    if is_labeled(text):
        return [parse_labeled(text)]

    specs = []
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        specs.append(parse_line(line))

    if not specs:
        raise RunSyntaxError(USAGE)
    return specs


def describe(spec: RunSpec, dry_run: bool = False) -> str:
    """Spell out the direction so a mistake is obvious before it copies."""
    lines = [
        f"  FROM  {spec.user1}  on {spec.host1}",
        f"  TO    {spec.user2}  on {spec.host2}",
    ]
    if dry_run:
        lines.append("  MODE  dry run, nothing will be written")
    return "\n".join(lines)
