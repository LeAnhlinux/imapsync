# imapsync Web UI — External API

HTTP API for driving mailbox migrations from your own programs: start a sync,
watch it, get told when it finishes. The web frontend is a client of this same
API, so anything the UI can do, a script can do.

To drive it from Claude or another MCP client instead of code, see
[`web/mcp`](web/mcp/README.md) — an MCP server built on this API.

- **Base URL** — `https://your-domain/api`
- **Content type** — `application/json` on every request and response, except
  the one multipart upload endpoint
- **Timestamps** — naive local time, `2026-07-29T08:02:44.247921`, in the
  server's configured zone (UTC+7). No timezone suffix, deliberately: the
  browser renders them as local time.

---

## 1. Authentication

Two credentials are accepted on the same header.

### API keys (for programs)

Create one in the web UI under **API Access**. The key is shown **once**, at
creation, and is not recoverable afterwards — only a SHA-256 digest is stored.
Keys look like `isk_` followed by 43 URL-safe base64 characters.

Send it either way:

```
Authorization: Bearer isk_pGzH9almMKJF...
X-API-Key: isk_pGzH9almMKJF...
```

### Session tokens (for the browser)

`POST /api/auth/login` returns a JWT valid for 24 hours. Programs should use an
API key instead: a token expires, cannot be scoped, and cannot be revoked
without changing the account password.

```bash
curl -X POST https://your-domain/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"..."}'
```

```json
{ "access_token": "eyJhbGciOi...", "token_type": "bearer" }
```

> Self-registration is disabled. `POST /api/auth/register` always returns 403;
> accounts are created on the server with `./createadmin`.

### Scopes

Scopes apply to API keys only — a browser session already authenticated as the
user and is not scope-limited.

| Scope | Grants |
|---|---|
| `sync:read` | list and inspect jobs, read logs, read history and stats, read a batch |
| `sync:write` | start and abort syncs |
| `profiles:read` | list and read profiles, browse folders |
| `profiles:write` | create, update, delete profiles |
| `batch:write` | upload and start batches |
| `schedules:read` | list schedules |
| `schedules:write` | create, update, delete schedules |
| `admin` | everything above, plus settings, API keys and webhooks |

A key without the scope a route needs gets **403**:

```json
{ "detail": "This API key does not carry the \"sync:write\" scope" }
```

Give a key the narrowest set that does its job. A bot that only kicks off
migrations needs `sync:write` and `sync:read`, not `admin`.

### Rate limits

Every key carries a limit in requests per minute, set when you create it
(default 60, maximum 10000). Windows are fixed and per-key. Every response
carries the current state:

```
X-Ratelimit-Limit: 50
X-Ratelimit-Remaining: 49
X-Ratelimit-Reset: 1785290036      # unix seconds when the window resets
```

Over the limit is **429**, with `Retry-After` in seconds:

```json
{ "detail": "Rate limit exceeded for this API key" }
```

Header names are case-insensitive per HTTP; the server emits them as
`X-Ratelimit-*`.

Note that requests rejected for a missing scope still consume budget — the
limit is checked before the scope.

`POST /api/auth/login` is throttled separately, per client address and
username: ten failed attempts in five minutes gives **429** with a plain
message and no `X-Ratelimit-*` headers.

```json
{ "detail": "Too many failed login attempts. Try again in a few minutes." }
```

### Request size

JSON bodies are capped at 1 MiB and the batch CSV endpoints at 16 MiB; over
either is **413**. The proxy in front enforces the same limits, so an oversized
body is refused before it reaches the application.

### Errors

Errors always carry a `detail` string:

```json
{ "detail": "Profile not found" }
```

| Status | Meaning |
|---|---|
| 400 | malformed body, or an option that is not allowed |
| 401 | missing, invalid, revoked or expired credential |
| 403 | authenticated, but the key lacks the scope (or the route is admin-only) |
| 404 | no such object, or it belongs to someone else |
| 409 | the job is still running; stop it before deleting |
| 429 | rate limited |
| 502 | an upstream IMAP server could not be reached |

---

## 2. Starting a sync

### `POST /api/sync/start` — scope `sync:write`

Two forms. Either name a stored profile:

```json
{ "profile_id": 1, "dry_run": false }
```

Or pass credentials directly:

```json
{
  "host1": "mail.old.com",
  "user1": "me@old.com",
  "password1": "secret",
  "ssl1": true,
  "host2": "mail.new.com",
  "user2": "me@new.com",
  "password2": "secret",
  "ssl2": true,
  "folders": ["INBOX", "Sent"],
  "extra_options": { "automap": true, "maxage": 30 },
  "dry_run": false
}
```

| Field | Type | Notes |
|---|---|---|
| `profile_id` | int | if set, credential fields are ignored; `extra_options` is merged over the profile's saved options for this run |
| `host1/user1/password1` | string | source mailbox; all required without a profile |
| `host2/user2/password2` | string | destination mailbox |
| `ssl1`, `ssl2` | bool | default `true` |
| `folders` | string[] | omit to sync everything |
| `extra_options` | object | see [Allowed options](#3-allowed-imapsync-options) |
| `dry_run` | bool | connect and report, write nothing |

Returns the job (HTTP 200):

```json
{
  "id": 7,
  "user_id": 1,
  "profile_id": null,
  "source_account": "me@old.com (mail.old.com)",
  "dest_account": "me@new.com (mail.new.com)",
  "status": "running",
  "pid": 41729,
  "started_at": "2026-07-29T08:02:44.247921",
  "finished_at": null,
  "exit_code": null,
  "messages_transferred": 0,
  "bytes_transferred": 0,
  "errors_count": 0,
  "summary": null
}
```

`status` is one of `running`, `completed`, `failed`, `aborted`, `interrupted`.
`interrupted` means the server restarted mid-sync — the process is gone and the
job did not finish.

### Other sync endpoints

| Method | Path | Scope | Purpose |
|---|---|---|---|
| GET | `/api/sync/running` | `sync:read` | jobs running right now |
| GET | `/api/sync/{id}` | `sync:read` | one job; `?wait=N` holds the request until it stops running (see [Waiting](#waiting)) |
| GET | `/api/sync/{id}/log` | `sync:read` | the log — whole, the last lines, or a page (see [Reading the log](#reading-the-log)) |
| POST | `/api/sync/{id}/abort` | `sync:write` | SIGTERM, escalating to SIGKILL after 5s |
| DELETE | `/api/sync/{id}` | `sync:write` | delete the job and its saved log |

### Reading the log

Without parameters, `GET /api/sync/{id}/log` returns the whole log. A finished
migration's log runs to megabytes, so a program usually wants part of it:

| Query | Returns |
|---|---|
| *(none)* | the whole log |
| `?tail=200` | the last 200 lines (at most 5000) |
| `?offset=N` | up to 256 KB from byte `N`, ending on a line boundary |

```json
{ "log": "...", "size": 1843920, "next_offset": 1843920 }
```

`size` is the log's length in bytes now. To follow a running job, start with
`?tail=50` and then keep asking for `?offset=<next_offset>`; an empty `log`
means nothing new has been written yet. To read a whole log in pages, start at
`?offset=0` and stop when `next_offset` equals `size`.

An aborted job keeps `status: "aborted"` — imapsync exits non-zero when
signalled, and that is not recorded as a failure.

---

## 3. Allowed imapsync options

`extra_options` is spliced into imapsync's command line, so it is filtered
against an allowlist. Anything else is a **400** naming what was rejected:

```json
{ "detail": "Unsupported or unsafe imapsync options: pipemess" }
```

**Flags** (pass `true`): `automap`, `usecache`, `nousecache`, `useuid`,
`nouseuid`, `subscribe`, `subscribeall`, `nosubscribe`, `syncinternaldates`,
`nosyncinternaldates`, `idatefromheader`, `syncacls`, `addheader`,
`allowsizemismatch`, `skipcrossduplicates`, `skipemptyfolders`,
`checkfoldersexist`, `nocheckfoldersexist`, `checkmessageexists`,
`foldersizes`, `nofoldersizes`, `foldersizesatend`, `nofoldersizesatend`,
`delete1`, `delete2`, `delete2duplicates`, `expunge1`, `expunge2`,
`noexpunge1`, `noexpunge2`, `justfolders`, `justfoldersizes`, `justconnect`,
`justlogin`, `justbanner`, `gmail1`, `gmail2`, `office1`, `office2`,
`exchange1`, `exchange2`, `domino1`, `domino2`

**Integers**: `maxage`, `minage`,
`maxsize`, `minsize`, `maxmessagespersecond`, `maxbytespersecond`, `timeout`,
`timeout1`, `timeout2`, `port1`, `port2`

**Strings**: `folder`, `include`, `exclude`, `prefix1`, `prefix2`, `sep1`,
`sep2`, `authmech1`, `authmech2`, `useheader`

**Rejected on purpose.** `pipemess` and `pipemesscheck` run shell commands;
`regexmess`, `regextrans2` and `regexflag` are evaluated as Perl. Host, user,
password, ssl, tmpdir, pidfile and logfile are managed by the server — allowing
them would let a caller redirect credentials or overwrite files. `debugimap`
prints the IMAP dialogue, including the login line.

`parallel`, `pipeline`, `batchsize` and `condstore` are this fork's own
additions and are **not** accepted here. A review of what they do found that
`--parallel` rebuilds the worker command line by hand and passes
`--password1 <plaintext>`, putting both mailbox passwords in `ps` output for
the length of the run, while dropping `maxage`, `exclude`, `port` and
`automap` and leaving workers that abort cannot reach; `--pipeline` fetches
message bodies with a different IMAP method than the normal path, and nobody
has shown the two produce identical bytes; `--condstore` writes a cache that
nothing reads. They remain available to whoever runs `imapsync` directly on
the command line.

`delete1` and `delete2` **are** allowed: deleting from the source after a
migration is a legitimate use, and the credentials are yours. Be deliberate.

---

## 4. Watching a job

Three ways, in order of how much work they cost you.

### Webhooks — the server calls you

Register an endpoint under **API Access**. Nothing polls, and the payload has
everything the job finished with.

Delivery is a `POST` with:

```
Content-Type: application/json
User-Agent: imapsync-web/1.0
X-Imapsync-Event: job.completed
X-Imapsync-Signature: sha256=<hex>
```

```json
{
  "event": "job.completed",
  "timestamp": "2026-07-29T08:53:14+07:00",
  "job": {
    "id": 2,
    "status": "completed",
    "source_account": "me@old.com (mail.old.com)",
    "dest_account": "me@new.com (mail.new.com)",
    "messages_transferred": 1420,
    "bytes_transferred": 98765432,
    "errors_count": 0,
    "exit_code": 0,
    "started_at": "2026-07-29T08:52:01+07:00",
    "finished_at": "2026-07-29T08:53:14+07:00",
    "summary": "..."
  }
}
```

Events: `job.completed`, `job.failed`, `job.aborted`, `job.started`, or `*` for
all of them.

**Verify the signature.** It is an HMAC-SHA256 of the exact request body, keyed
with the secret shown once when you created the webhook. Anyone who learns your
URL can post to it; only you can produce a valid signature.

```python
import hashlib, hmac

def verify(body: bytes, header: str, secret: str) -> bool:
    want = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(header, want)   # constant time, not ==
```

```javascript
import crypto from 'node:crypto'

function verify(body, header, secret) {
  const want = 'sha256=' + crypto.createHmac('sha256', secret).update(body).digest('hex')
  return crypto.timingSafeEqual(Buffer.from(header), Buffer.from(want))
}
```

Answer `2xx` promptly. A non-2xx or a timeout is retried twice, after 1s and
3s, then given up on — the outcome stays available through the API either way.
The last status and time are shown in the UI.

### WebSocket — live log and progress

```
wss://your-domain/api/ws/jobs/{id}?token=<jwt>
```

Takes a **session JWT**, not an API key: it is what the browser has. The socket
replays the log so far, then streams messages until the job ends.

```json
{"type": "log",      "data": "Host1 folder    4/15 [Deleted Items]   Size: 2239 ..."}
{"type": "progress", "percent": 62.5, "folder": "7,200/11,520 messages", "eta": "1h12m0s",
 "messages": 7200, "bytes": 890123456}
{"type": "status",   "status": "completed", "exit_code": 0}
```

`progress` arrives at most once a second. `messages` and `bytes` are the running
totals, counted from imapsync's own per-message output, and `folder` reads
`sizing 4/15` during the several minutes before the first message moves — on a
9,000-message account that phase alone takes four to five minutes, and nothing
else is emitted while it runs.

You may only open a socket for your own jobs; anyone else's is 403.

### Waiting

`GET /api/sync/{id}?wait=60` answers as soon as the job is no longer
`running`, or after 60 seconds with the job still running — whichever comes
first. `wait` is in seconds, at most 120. One waiting request costs one request
against the key's rate limit, so a loop of these is the cheapest way to block
until a sync finishes:

```bash
until [ "$(curl -s "$BASE/sync/$JOB?wait=120" -H "X-API-Key: $KEY" | jq -r .status)" != "running" ]; do :; done
```

### Polling

`GET /api/sync/{id}` until `status` is no longer `running`. Simplest, but mind
the rate limit — a 60/min key polling every second will exhaust itself.

---

## 5. Profiles

A profile stores one mailbox pair, with the passwords encrypted at rest. The
API never returns them.

| Method | Path | Scope |
|---|---|---|
| GET | `/api/profiles` | `profiles:read` |
| GET | `/api/profiles/{id}` | `profiles:read` |
| POST | `/api/profiles` | `profiles:write` |
| PUT | `/api/profiles/{id}` | `profiles:write` |
| DELETE | `/api/profiles/{id}` | `profiles:write` |

```json
{
  "name": "Company A",
  "host1": "mail.old.com", "user1": "me@old.com", "password1": "secret", "ssl1": true,
  "host2": "mail.new.com", "user2": "me@new.com", "password2": "secret", "ssl2": true,
  "extra_options": { "automap": true }
}
```

On `PUT`, omitting a password keeps the stored one.

### `POST /api/folders/browse` — scope `profiles:read`

Lists a mailbox's folders and their sizes, for building a `folders` selection.

**This is slow.** It opens a real IMAP connection and SELECTs every folder; a
Gmail account with ~9,000 messages was measured at four to five minutes. The
request is held open for up to six minutes before giving up, so call it
asynchronously and tell your user to wait.

```json
{ "host": "mail.old.com", "user": "me@old.com", "password": "secret", "ssl": true }
```

```json
[{ "name": "INBOX", "messages": 1420, "size": 98765432 }]
```

**Asynchronous form.** Add `"async": true` and the request answers at once
with **202** and a task, instead of being held open:

```json
{ "profile_id": 1, "async": true }
```

```json
{ "id": "5f0c1e…", "status": "running" }
```

Then poll `GET /api/folders/browse/{id}` (scope `profiles:read`) until `status`
is `done` — with `folders` holding the list above — or `failed`, with `error`.
A listing of the same mailbox that is still running is returned rather than
started twice, so retrying is safe. A finished listing stays readable for 15
minutes.

---

## 6. Batches

For migrating many mailboxes. Rows run one at a time — several concurrent
imapsync processes against one server tend to hit connection limits.

### `POST /api/batch/upload-text` — scope `batch:write`

```json
{ "csv_text": "mail.old.com;u1@old.com;pw1;mail.new.com;u1@new.com;pw2\n..." }
```

Format is semicolon-separated, one mailbox per line, with an optional seventh
options column. Lines starting with `#` are comments.

```
host1;user1;password1;host2;user2;password2[;--dry --maxage 30]
```

Query parameters set defaults for every row: `dry_run`, `ssl1`, `ssl2`,
`automap`, `usecache` (booleans; `automap` and `usecache` default to true).

`POST /api/batch/upload` takes the same content as a multipart `file` field.

Returns a **pending** batch. Nothing runs until you start it:

```json
{ "id": 3, "filename": "paste.csv", "total_rows": 40, "completed_rows": 0,
  "failed_rows": 0, "status": "pending", "created_at": "..." }
```

| Method | Path | Scope | Purpose |
|---|---|---|---|
| POST | `/api/batch/{id}/start` | `batch:write` | begin running; **409** if it already is |
| POST | `/api/batch/{id}/abort` | `batch:write` | stop it; aborts the mailbox in flight and skips the rest |
| GET | `/api/batch/{id}` | `sync:read` | poll `completed_rows` / `failed_rows` |

Each row becomes an ordinary sync job, so webhooks fire per mailbox.

Credentials in an uploaded batch are encrypted at rest and erased when the run
finishes.

---

## 7. Schedules

Run a profile on a cron schedule. Expressions are standard five-field crontab
and are validated on save.

| Method | Path | Scope |
|---|---|---|
| GET | `/api/schedules` | `schedules:read` |
| POST | `/api/schedules` | `schedules:write` |
| PUT | `/api/schedules/{id}` | `schedules:write` |
| DELETE | `/api/schedules/{id}` | `schedules:write` |

```json
{ "profile_id": 1, "cron_expression": "0 3 * * *", "is_active": true }
```

---

## 8. History

### `GET /api/history` — scope `sync:read`

Query: `page` (default 1), `per_page` (default 20, max 100), `status`, `q`
(matches summary, source and destination).

The total row count is in the `X-Total-Count` response header.

### `POST /api/history/purge` — scope `sync:write`

Deletes finished jobs and their logs. Running jobs are never touched.

```json
{ "older_than_days": 30 }
```

`0` clears every finished job. Returns the count:

```json
{ "deleted": 12 }
```

Logs are the reason this exists: a job row is a few hundred bytes, but its
imapsync output can be megabytes, and nothing else removes them.

### History retention

A saved policy that the server applies on its own, every night at 03:30 local
time. Off by default — a tool that starts deleting history unasked would be a
surprise. Running jobs are never touched.

| Method | Path | Scope |
|---|---|---|
| GET | `/api/history/retention` | `sync:read` |
| PUT | `/api/history/retention` | `sync:write` |
| POST | `/api/history/retention/run` | `sync:write` |

```json
{ "enabled": true, "keep_days": 30 }
```

`keep_days` must be between 1 and 3650. The response also carries when the
sweep last ran and how much it removed:

```json
{ "enabled": true, "keep_days": 30,
  "last_run_at": "2026-07-29T03:30:02.114000", "last_deleted": 12 }
```

`POST .../run` applies the saved policy immediately and returns
`{"deleted": n}` — the only way to confirm the setting does what you expect
without waiting a day. It is a 400 while the policy is switched off.

### `GET /api/history/stats` — scope `sync:read`

```json
{ "total_syncs": 128, "successful": 121, "failed": 7,
  "total_messages": 184320, "total_bytes": 9876543210, "success_rate": 94.5 }
```

---

## 9. Managing keys and webhooks

All of these need the `admin` scope, or a browser session.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/api-keys` | list (never includes the keys themselves) |
| POST | `/api/api-keys` | create; the response is the only place the key appears |
| GET | `/api/api-keys/scopes` | valid scope names |
| PUT | `/api/api-keys/{id}` | change name, scopes or rate limit; the key itself is unchanged |
| POST | `/api/api-keys/{id}/revoke` | stop it working, keep the record |
| DELETE | `/api/api-keys/{id}` | remove it entirely |
| GET | `/api/webhooks` | list |
| POST | `/api/webhooks` | create; the response is the only place the secret appears |
| GET | `/api/webhooks/events` | valid event names |
| PUT | `/api/webhooks/{id}` | change url, events, or enable/disable |
| POST | `/api/webhooks/{id}/test` | send a synthetic delivery |
| DELETE | `/api/webhooks/{id}` | remove |

```json
{ "name": "migration-bot", "scopes": ["sync:read", "sync:write"],
  "rate_limit_per_minute": 120, "expires_in_days": 90 }
```

`expires_in_days: 0` means it never expires.

---

## 10. A worked example

Migrating one mailbox and being told when it is done:

```bash
KEY="isk_..."
BASE="https://your-domain/api"

JOB=$(curl -s -X POST "$BASE/sync/start" \
  -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{
        "host1":"mail.old.com","user1":"me@old.com","password1":"pw1",
        "host2":"mail.new.com","user2":"me@new.com","password2":"pw2",
        "extra_options":{"automap":true}
      }' | jq -r .id)

echo "started job $JOB"
```

With a webhook registered, that is the whole client. Without one:

```bash
until [ "$(curl -s "$BASE/sync/$JOB?wait=120" -H "X-API-Key: $KEY" | jq -r .status)" != "running" ]; do :; done
curl -s "$BASE/sync/$JOB" -H "X-API-Key: $KEY" | jq '{status, messages_transferred, errors_count}'
```

Python, using webhooks:

```python
import os, requests

BASE = "https://your-domain/api"
S = requests.Session()
S.headers["X-API-Key"] = os.environ["IMAPSYNC_KEY"]

def migrate(src, dst):
    r = S.post(f"{BASE}/sync/start", json={
        "host1": src["host"], "user1": src["user"], "password1": src["password"],
        "host2": dst["host"], "user2": dst["user"], "password2": dst["password"],
        "extra_options": {"automap": True},
    })
    r.raise_for_status()
    return r.json()["id"]
```

---

## 11. Handling credentials

The API takes mailbox passwords, so:

- **Use HTTPS.** The deploy scripts set up Let's Encrypt; without TLS every
  password crosses the network in the clear.
- **Keep API keys out of source control** and out of shell history. Prefer an
  environment variable or a secrets manager over a literal.
- **Scope and expire keys.** A key that can only start syncs cannot read your
  settings or mint more keys.
- **Revoke rather than delete** when you suspect a leak — revoking keeps the
  record of what the key was and when it was last used.
- **Stored passwords are encrypted at rest** with the instance's
  `IMAPSYNC_FERNET_KEY`. Losing that key makes every stored profile and pending
  batch undecryptable; back up `.env` alongside the database.
- **Job logs contain message subjects and headers.** `GET /api/sync/{id}/log`
  and the WebSocket are restricted to the job's owner for that reason.

---

## 12. MCP server: architecture

`web/mcp` is an [MCP](https://modelcontextprotocol.io) server built entirely
on this API: no database, no shared code with the backend, no endpoint of its
own. This section describes it closely enough to rebuild it elsewhere — in
particular as a **central gateway** that manages several imapsync servers from
one place (see [12.9](#129-building-a-central-gateway)). For setup and day-to-day use, see
[`web/mcp/HUONG_DAN.md`](web/mcp/HUONG_DAN.md) (Vietnamese) and
[`web/mcp/README.md`](web/mcp/README.md).

### 12.1 Topology

```
 user's machine                                   imapsync server
┌─────────────────────────────────────┐          ┌──────────────────────────────┐
│ Claude Code / Claude Desktop        │          │ nginx :443                   │
│   │  MCP JSON-RPC over stdio        │          │   │                          │
│   ▼                                 │  HTTPS   │   ▼                          │
│ imapsync-mcp (one Go binary) ───────┼─────────►│ imapsync-web /api/*          │
│   • 46 tools                        │ X-API-Key│   • scopes, rate limit       │
│   • delete-mail guard               │          │   • runner → imapsync (Perl) │
│   • polling for slow calls          │          │   • SQLite, job logs         │
└─────────────────────────────────────┘          └──────────────────────────────┘
```

- **Process model.** The client launches the binary and talks to it over
  stdin/stdout. One process per client session; it holds no state between
  calls, so it can be killed and restarted at any time.
- **Where work happens.** Every sync runs on the server. The MCP server only
  makes HTTP calls, so closing the client never stops a job.
- **Trust boundary.** The API key decides what is possible (scopes, rate
  limit). The MCP server narrows it further — the delete guard — but never
  widens it.

### 12.2 Code layout

| File | Contents |
|---|---|
| `main.go` | env config, `newServer()` (name `imapsync`, version, server instructions), stdio transport |
| `client.go` | `Client`: base URL normalisation, `X-API-Key`, JSON in/out, error mapping |
| `tools.go` | the tools with logic of their own: profiles, folders, sync, logs, history; the delete guard |
| `tools_more.go` | `apiTool()` — the generic one-call tool — and the 36 tools built on it |
| `server_test.go` | every behaviour against a fake API, through a real MCP client (in-memory transport) |
| `e2e_test.go` | every tool against a real server; fails if a tool is added without coverage |
| `manifest.sh`, `Makefile` | cross-builds and the download manifest |

Dependencies: Go 1.25, `github.com/modelcontextprotocol/go-sdk` v1.8.0, and
the standard library.

### 12.3 Configuration

| Variable | Required | Meaning |
|---|---|---|
| `IMAPSYNC_URL` | yes | site root, with or without `/api`; normalised to `<root>/api` |
| `IMAPSYNC_API_KEY` | yes | must start with `isk_` — a session JWT is refused at startup |
| `IMAPSYNC_MCP_ALLOW_DELETE` | no | `1` turns the delete guard off |

Logs go to stderr only: stdout is the protocol channel.

### 12.4 The request path

One function, `Client.do(ctx, method, path, query, body, out)`, makes every
call:

1. URL = `base + path`, plus `?query` when given.
2. Headers: `X-API-Key: <key>`; `Content-Type: application/json` when there
   is a body.
3. HTTP client timeout **150 s** — above the longest server hold
   (`GET /sync/{id}?wait=120`).
4. Status ≥ 300 becomes `APIError{Status, Detail}`, `Detail` taken from the
   body's `{"detail": ...}` (raw body if absent). The tool returns it as an
   MCP error result: `isError: true`, text
   `imapsync API answered 403: This API key does not carry the "sync:write" scope`.
   The model reads that and can tell the user what to fix.
5. Success: the JSON body is passed back to the model unchanged (or `{"status":"ok"}`
   for an empty body). No reshaping, except `list_profiles` / `create_profile`,
   which summarise a profile as `{id, name, source: "user @ host", destination, extra_options}`.

Tool annotations: read-only tools carry `readOnlyHint`; tools that delete or
stop something carry `destructiveHint: true`, which makes clients such as
Claude Code ask before calling them; all carry `openWorldHint`.

### 12.5 Tool catalog

Every endpoint in the [appendix](#appendix-every-endpoint) has a tool except
`POST /batch/upload` (the multipart twin of `create_batch`) and the job
WebSocket (replaced by `get_job` + `get_job_log`). IDs in paths are integers;
parameters marked ? are optional.

| Tool | Calls | Scope | Arguments / behaviour |
|---|---|---|---|
| `whoami` | `GET /auth/me` | — | |
| `list_profiles` | `GET /profiles` | profiles:read | summarised |
| `get_profile` | `GET /profiles/{id}` | profiles:read | `profile_id` |
| `create_profile` | `POST /profiles` | profiles:write | `name`, `source`, `destination`, `extra_options?` — see 12.6 |
| `update_profile` | `PUT /profiles/{id}` | profiles:write | `profile_id`, `name?`, `source?`, `destination?` (every field optional), `extra_options?` |
| `delete_profile` | `DELETE /profiles/{id}` | profiles:write | destructive |
| `browse_folders` | `POST /folders/browse` + `GET /folders/browse/{task}` | profiles:read | `profile_id` or `mailbox` — see 12.6 |
| `start_sync` | `GET /profiles/{id}` (guard) + `POST /sync/start` | sync:write | see 12.6 |
| `get_job` | `GET /sync/{id}?wait=` | sync:read | `job_id`, `wait_seconds?` capped at 120 |
| `get_job_log` | `GET /sync/{id}/log` | sync:read | `job_id`, `offset?` → `?offset=`; else `tail?` (default 100) → `?tail=` |
| `list_running_jobs` | `GET /sync/running` | sync:read | |
| `abort_job` | `POST /sync/{id}/abort` | sync:write | destructive |
| `delete_job` | `DELETE /sync/{id}` | sync:write | destructive |
| `list_history` | `GET /history` | sync:read | `status?`, `query?` → `q`, `page?`, `per_page?` (default 20, cap 100) |
| `history_stats` | `GET /history/stats` | sync:read | |
| `purge_history` | `POST /history/purge` | sync:write | `older_than_days` (required; 0 = all finished); destructive |
| `get_history_retention` | `GET /history/retention` | sync:read | |
| `set_history_retention` | `PUT /history/retention` | sync:write | `enabled`, `keep_days`; destructive |
| `run_history_retention` | `POST /history/retention/run` | sync:write | destructive |
| `create_batch` | `POST /batch/upload-text?…` | batch:write | `csv_text`; `dry_run?`, `ssl1?`, `ssl2?`, `automap?`, `usecache?` → query string; delete guard on the CSV |
| `start_batch` | `POST /batch/{id}/start` | batch:write | `batch_id` |
| `get_batch` | `GET /batch/{id}` | sync:read | `batch_id` |
| `abort_batch` | `POST /batch/{id}/abort` | batch:write | destructive |
| `list_schedules` | `GET /schedules` | schedules:read | |
| `create_schedule` | `POST /schedules` | schedules:write | `profile_id`, `cron_expression`, `is_active?` |
| `update_schedule` | `PUT /schedules/{id}` | schedules:write | `schedule_id`, `cron_expression?`, `is_active?` |
| `delete_schedule` | `DELETE /schedules/{id}` | schedules:write | destructive |
| `get_email_notifications` | `GET /notifications/settings` | admin | |
| `update_email_notifications` | `PUT /notifications/settings` | admin | any of `smtp_host`, `smtp_port`, `smtp_user`, `smtp_password`, `from_email`, `to_email`, `notify_on_success`, `notify_on_failure` |
| `test_email_notifications` | `POST /notifications/test` | admin | |
| `get_telegram` | `GET /telegram/settings` | admin | |
| `update_telegram` | `PUT /telegram/settings` | admin | any of `enabled`, `bot_token`, `allowed_chat_ids`, `user_id`, `notify_on_success`, `notify_on_failure`, `notify_on_progress`, `progress_step` |
| `telegram_status` | `GET /telegram/status` | admin | |
| `test_telegram` | `POST /telegram/test` | admin | |
| `list_api_keys` | `GET /api-keys` | admin | |
| `list_api_key_scopes` | `GET /api-keys/scopes` | admin | |
| `create_api_key` | `POST /api-keys` | admin | `name`, `scopes`, `rate_limit_per_minute?`, `expires_in_days?`; the key is in the answer only |
| `update_api_key` | `PUT /api-keys/{id}` | admin | `key_id`, `name?`, `scopes?`, `rate_limit_per_minute?` |
| `revoke_api_key` | `POST /api-keys/{id}/revoke` | admin | destructive |
| `delete_api_key` | `DELETE /api-keys/{id}` | admin | destructive |
| `list_webhooks` | `GET /webhooks` | admin | |
| `list_webhook_events` | `GET /webhooks/events` | admin | |
| `create_webhook` | `POST /webhooks` | admin | `url`, `events?`, `is_active?`; the secret is in the answer only |
| `update_webhook` | `PUT /webhooks/{id}` | admin | `webhook_id`, `url?`, `events?`, `is_active?` |
| `test_webhook` | `POST /webhooks/{id}/test` | admin | |
| `delete_webhook` | `DELETE /webhooks/{id}` | admin | destructive |

The one-call tools are declared, not hand-written: `apiTool(server, tool,
build)` takes a function from the typed arguments to `{method, path, query,
body}`. For updates, `bodyOf(input, "<id field>")` marshals the arguments and
drops the path ID; optional fields are pointers with `omitempty`, so only what
the model set is sent and the API keeps the rest (`PUT` here means *patch*).

### 12.6 Tools with behaviour of their own

**Mailbox arguments.** `source`, `destination` and `mailbox` share one shape:

```json
{ "host": "imap.example.com", "user": "me@example.com", "password": "…",
  "ssl": true, "port": 993 }
```

`ssl` defaults to true; `port` is optional. They flatten to the API's form:
`host1/user1/password1/ssl1`, `host2/…/ssl2`, and a port becomes the imapsync
option `port1` / `port2` inside `extra_options`. Host, user and password are
required (trimmed); otherwise the tool fails before calling the API.

**`start_sync`** takes either `profile_id` or `source` + `destination`, plus
`folders?`, `dry_run?`, `extra_options?`.

- By profile: `{profile_id, dry_run, folders?, extra_options?}`. Unless it is a
  dry run or the guard is off, it first reads `GET /profiles/{id}` and checks
  the profile's saved options merged with the call's — saved options apply to
  the run, so checking only the call would let a stored `delete1` through.
- By hand: the flattened pair plus `dry_run`, `folders?`; the guard checks the
  call's options.
- Neither given: an error naming what to pass.

**`browse_folders`** takes `profile_id` or `mailbox`:

1. `POST /folders/browse` with `"async": true` → `202 {id, status:"running"}`.
2. Poll `GET /folders/browse/{id}` every **3 s** for up to **45 s**.
3. `done` → `{status:"done", folders:[{name, messages, size}]}`;
   `failed` → tool error with the server's message;
   still running → `{status:"running", note:"call browse_folders again with the same arguments"}`.

Calling again is cheap: the server returns the listing already running for the
same user and mailbox instead of starting another, and keeps finished results
for 15 minutes (in memory — a server restart loses them, answered with 404).

**`get_job`** with `wait_seconds` maps to `?wait=` (cap 120): the server holds
the request until the job leaves `running`. A client keeps waiting by calling
again; each call costs one request of rate limit.

**`get_job_log`** defaults to the last 100 lines. To follow a live job:
`tail` once, then `offset = next_offset` repeatedly; an empty `log` means
nothing new.

### 12.7 The delete guard

Refuses, unless `IMAPSYNC_MCP_ALLOW_DELETE=1` or the run is a dry run, any of
`delete1`, `delete2`, `delete2duplicates`, `expunge1`, `expunge2` that is
*switched on*: present and not `null`, `false`, `0`, `""`, `"0"` or `"false"`.

| Where | What is checked |
|---|---|
| `start_sync` by profile | profile's saved options ∪ the call's |
| `start_sync` by hand | the call's options |
| `create_profile`, `update_profile` | the options being saved (a dry run does not exempt these) |
| `create_batch` | the CSV text, regex `--(delete1\|delete2\|delete2duplicates\|expunge1\|expunge2)\b`, unless `dry_run` |

The error says which option and how to allow it. The server accepts these
options on purpose (deleting from the source after a migration is legitimate);
the guard exists because a model should not reach for them on its own.

### 12.8 Distribution and tests

- `make downloads` cross-builds `imapsync-mcp-{darwin-arm64,darwin-amd64,linux-amd64,windows-amd64.exe}`,
  copies `HUONG_DAN.md`, and writes `manifest.json`:
  ```json
  { "version": "0.2.0", "guide": "HUONG_DAN.md",
    "files": [{ "name": "imapsync-mcp-darwin-arm64", "os": "darwin", "arch": "arm64",
                "size": 8034770, "sha256": "…" }] }
  ```
- `deploy.sh` puts that directory at `/downloads/mcp/`; the API Access page
  reads the manifest to show download buttons and the install commands.
- `go test ./...` runs against a fake API. `TestEndToEnd`
  (`IMAPSYNC_E2E_URL`, `IMAPSYNC_E2E_KEY` with `admin`) drives all 46 tools
  against a real server — run it on a throwaway instance whose
  `IMAPSYNC_PATH` is a stub.

### 12.9 Building a central gateway

To manage several imapsync servers from one place, put a gateway between the
clients and the servers. Nothing on the imapsync side needs to change: each
server is reached through this API with its own key.

```
 Claude / scripts / dashboards
            │  MCP (Streamable HTTP) and/or HTTP
            ▼
   ┌───────────────────────────┐        ┌──────────────────────┐
   │ gateway                   │──key A─►│ imapsync server A    │
   │  • instance registry      │──key B─►│ imapsync server B    │
   │  • its own users + auth   │──key C─►│ imapsync server C    │
   │  • routing, aggregation   │        └──────────────────────┘
   │  • webhook receiver ◄─────┼──── job.* events from every server
   │  • audit log, policy      │
   └───────────────────────────┘
```

**What carries over unchanged**

- The request path (12.4), the mailbox shape and flattening (12.6), the
  polling for `browse_folders`, `wait` and log paging, and the delete guard
  (12.7) — these are about the API, not about where the caller runs.
- The tool catalog (12.5), each tool gaining one argument (below).

**Instance registry.** One record per server:
`{id, name, base_url, api_key (encrypted at rest), scopes, enabled}`. Issue
each server's key in its own **API Access** page with only the scopes the
gateway needs; `update_api_key` / **Edit** changes scopes later without
replacing the key. Check a new record with `GET /auth/me` (and
`GET /api-keys/scopes`, which answers 403 unless the key has `admin`).

**Routing.** Every per-server tool takes an `instance` argument (name or id).
IDs — jobs, profiles, batches, schedules, keys, webhooks — are per-server
integers and collide across servers, so return and accept a qualified form
such as `"srvA:42"`, or always pair `instance` with the id.

**Tools only a gateway can have**

| Tool | Does |
|---|---|
| `list_instances` | registry entries with health (`GET /api/health`) and key validity |
| `list_running_jobs_all` | `GET /sync/running` on every instance, merged, each job tagged with its instance |
| `history_stats_all` | `GET /history/stats` everywhere; sum `total_syncs`, `successful`, `failed`, `total_messages`, `total_bytes`, recompute `success_rate` |
| `find_job` | search `GET /history?q=` across instances by account name |
| `pick_instance` | choose where to run a new sync, e.g. fewest running jobs |

**Events instead of polling.** Register one webhook per server
(`create_webhook`, `url` = the gateway's receiver, events `*`). Store each
signing secret against its instance; verify `X-Imapsync-Signature`
(`sha256=` HMAC-SHA256 of the raw body, constant-time compare) before trusting
a delivery; identify the instance by the secret that verifies, or by a path
such as `/hooks/<instance>`. Payload: [section 4](#webhooks--the-server-calls-you).
Deliveries retry twice (1 s, 3 s) — make the receiver idempotent on
`(instance, job.id, event)`. With events stored centrally, "what finished"
needs no polling at all; `get_job` with `wait_seconds` remains for a caller
that wants to block.

**Limits to design around**

- **Rate limit is per key** (default 60/min, max 10000). A fan-out tool makes
  one request per instance per call; give the gateway's keys a higher limit,
  and treat `429` + `Retry-After` per instance, not globally.
- **Timeouts.** Hold client requests at least 150 s if you proxy `?wait=`;
  never proxy the synchronous `/folders/browse` — use `"async": true`.
- **Batches** run rows one at a time per server. To spread a large migration,
  split the CSV across instances and create one batch on each.
- **Credentials.** Passwords given by hand reach the gateway and each server in
  request bodies. Keep the gateway on HTTPS end to end, never log request
  bodies, and prefer profiles (encrypted on each server) for repeat pairs.
- **Admin scope.** A key with `admin` can issue more keys on that server. Hold
  such keys only if the gateway really administers servers, and keep them out
  of the MCP-facing path otherwise.

**Gateway policy.** The gateway is the natural place for rules that span
servers: who may touch which instance, the delete guard applied once for
everyone, an audit log of every write (`start_sync`, `abort_job`, deletes) with
the gateway user who asked, and per-user quotas.

**Transport.** A central gateway runs as a service, so expose MCP over
**Streamable HTTP** (the Go SDK's `mcp.NewStreamableHTTPHandler`) instead of
stdio, behind the gateway's own authentication (OAuth or per-user tokens) —
never by sharing an imapsync API key with users.

---

## Appendix: every endpoint

| Method | Path | Scope |
|---|---|---|
| GET | `/api/health` | none |
| POST | `/api/auth/login` | none |
| POST | `/api/auth/register` | always 403 |
| GET | `/api/auth/me` | any |
| GET | `/api/profiles` | `profiles:read` |
| GET | `/api/profiles/{id}` | `profiles:read` |
| POST | `/api/profiles` | `profiles:write` |
| PUT | `/api/profiles/{id}` | `profiles:write` |
| DELETE | `/api/profiles/{id}` | `profiles:write` |
| POST | `/api/folders/browse` | `profiles:read` |
| GET | `/api/folders/browse/{id}` | `profiles:read` |
| POST | `/api/sync/start` | `sync:write` |
| POST | `/api/sync/{id}/abort` | `sync:write` |
| DELETE | `/api/sync/{id}` | `sync:write` |
| POST | `/api/history/purge` | `sync:write` |
| GET | `/api/history/retention` | `sync:read` |
| PUT | `/api/history/retention` | `sync:write` |
| POST | `/api/history/retention/run` | `sync:write` |
| GET | `/api/sync/running` | `sync:read` |
| GET | `/api/sync/{id}` | `sync:read` |
| GET | `/api/sync/{id}/log` | `sync:read` |
| GET | `/api/ws/jobs/{id}` | session JWT only |
| GET | `/api/history` | `sync:read` |
| GET | `/api/history/stats` | `sync:read` |
| POST | `/api/batch/upload` | `batch:write` |
| POST | `/api/batch/upload-text` | `batch:write` |
| POST | `/api/batch/{id}/start` | `batch:write` |
| POST | `/api/batch/{id}/abort` | `batch:write` |
| GET | `/api/batch/{id}` | `sync:read` |
| GET | `/api/schedules` | `schedules:read` |
| POST | `/api/schedules` | `schedules:write` |
| PUT | `/api/schedules/{id}` | `schedules:write` |
| DELETE | `/api/schedules/{id}` | `schedules:write` |
| GET/PUT | `/api/notifications/settings` | `admin` |
| POST | `/api/notifications/test` | `admin` |
| GET/POST | `/api/api-keys` | `admin` |
| GET | `/api/api-keys/scopes` | `admin` |
| PUT | `/api/api-keys/{id}` | `admin` |
| POST | `/api/api-keys/{id}/revoke` | `admin` |
| DELETE | `/api/api-keys/{id}` | `admin` |
| GET/POST | `/api/webhooks` | `admin` |
| GET | `/api/webhooks/events` | `admin` |
| PUT/DELETE | `/api/webhooks/{id}` | `admin` |
| POST | `/api/webhooks/{id}/test` | `admin` |
| GET/PUT | `/api/telegram/settings` | `admin` |
| GET | `/api/telegram/status` | `admin` |
| POST | `/api/telegram/test` | `admin` |
