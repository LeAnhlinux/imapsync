"""Allowlist for imapsync command-line options coming from user input.

`extra_options` is user-controlled (profile form, 7th CSV column) and is spliced
straight into imapsync's argv. imapsync exposes options that run shell commands
(`--pipemess`, `--pipemesscheck`) or eval Perl code (`--regexmess`,
`--regextrans2`, `--regexflag`), so anything not explicitly listed here is
dropped rather than passed through.

Options that the runner manages itself (host/user/password/ssl/tmpdir/pidfile...)
are deliberately absent: letting them through would let a caller redirect
credentials or overwrite files.
"""

import re

# name -> kind: "flag" (boolean), "int", or "str"
ALLOWED_OPTIONS: dict[str, str] = {
    # --- behaviour toggles ---
    "automap": "flag",
    "usecache": "flag",
    "nousecache": "flag",
    "useuid": "flag",
    "nouseuid": "flag",
    "subscribe": "flag",
    "subscribeall": "flag",
    "nosubscribe": "flag",
    "syncinternaldates": "flag",
    "nosyncinternaldates": "flag",
    "idatefromheader": "flag",
    "syncacls": "flag",
    "addheader": "flag",
    "allowsizemismatch": "flag",
    "skipcrossduplicates": "flag",
    "skipemptyfolders": "flag",
    "checkfoldersexist": "flag",
    "nocheckfoldersexist": "flag",
    "checkmessageexists": "flag",
    "foldersizes": "flag",
    "nofoldersizes": "flag",
    "foldersizesatend": "flag",
    "nofoldersizesatend": "flag",
    # --- destructive, but legitimate migration features ---
    "delete1": "flag",
    "delete2": "flag",
    "delete2duplicates": "flag",
    "expunge1": "flag",
    "expunge2": "flag",
    "noexpunge1": "flag",
    "noexpunge2": "flag",
    # --- "just do X and stop" modes ---
    "justfolders": "flag",
    "justfoldersizes": "flag",
    "justconnect": "flag",
    "justlogin": "flag",
    "justbanner": "flag",
    # --- server presets ---
    "gmail1": "flag",
    "gmail2": "flag",
    "office1": "flag",
    "office2": "flag",
    "exchange1": "flag",
    "exchange2": "flag",
    "domino1": "flag",
    "domino2": "flag",
    # --- performance knobs (see imapsync --parallel/--batchsize/--pipeline) ---
    "parallel": "int",
    "batchsize": "int",
    "pipeline": "int",
    "condstore": "flag",
    "nocondstore": "flag",
    # --- numeric limits ---
    "maxage": "int",
    "minage": "int",
    "maxsize": "int",
    "minsize": "int",
    "maxmessagespersecond": "int",
    "maxbytespersecond": "int",
    "timeout": "int",
    "timeout1": "int",
    "timeout2": "int",
    "port1": "int",
    "port2": "int",
    # --- string values ---
    "folder": "str",
    "include": "str",
    "exclude": "str",
    "prefix1": "str",
    "prefix2": "str",
    "sep1": "str",
    "sep2": "str",
    "authmech1": "str",
    "authmech2": "str",
    "useheader": "str",
}

# Rejected on purpose — kept explicit so the reason survives future edits.
DANGEROUS_OPTIONS = {
    # run a shell command
    "pipemess", "pipemesscheck",
    # eval'd as Perl code
    "regexmess", "regextrans2", "regexflag",
    "delete2foldersonly", "delete2foldersbutnot",
    # would redirect credentials or file writes
    "host1", "host2", "user1", "user2", "password1", "password2",
    "passfile1", "passfile2", "tmpdir", "pidfile", "logfile", "logdir",
    "ssl1", "ssl2", "tls1", "tls2",
    # dumps the IMAP dialogue, including the LOGIN line
    "debugimap", "debugimap1", "debugimap2",
}

_SAFE_VALUE = re.compile(r"^[^\x00-\x1f\x7f]{1,200}$")
# Perl regex code-embedding, blocked by default in interpolation but not worth the bet
_REGEX_CODE = re.compile(r"\(\?\??\{")


def sanitize_extra_options(extra: dict | None) -> tuple[dict, list[str]]:
    """Filter user-supplied imapsync options.

    Returns (accepted, rejected_names). Rejected entries are dropped silently by
    the caller's standards — it is up to the caller to surface them.
    """
    if not extra:
        return {}, []

    accepted: dict = {}
    rejected: list[str] = []

    for raw_key, value in extra.items():
        key = str(raw_key).strip().lstrip("-")
        kind = ALLOWED_OPTIONS.get(key)

        if kind is None:
            rejected.append(key)
            continue

        if kind == "flag":
            # Only pass the flag through when it is actually on.
            if isinstance(value, bool):
                if value:
                    accepted[key] = True
            elif str(value).lower() in ("1", "true", "yes", "on", ""):
                accepted[key] = True
            else:
                rejected.append(key)
            continue

        if isinstance(value, bool) or value is None:
            rejected.append(key)
            continue

        text = str(value).strip()
        if not text or not _SAFE_VALUE.match(text) or text.startswith("-"):
            rejected.append(key)
            continue

        if kind == "int":
            if not re.fullmatch(r"\d+", text):
                rejected.append(key)
                continue
        elif _REGEX_CODE.search(text):
            rejected.append(key)
            continue

        accepted[key] = text

    return accepted, rejected
