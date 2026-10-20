# imapsync Web UI — External API

HTTP API for driving mailbox migrations from your own programs: start a sync,
watch it, get told when it finishes. The web frontend is a client of this same
API, so anything the UI can do, a script can do.

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
| GET | `/api/sync/{id}` | `sync:read` | one job |
| GET | `/api/sync/{id}/log` | `sync:read` | `{"log": "..."}` — live buffer, or the saved file |
| POST | `/api/sync/{id}/abort` | `sync:write` | SIGTERM, escalating to SIGKILL after 5s |
| DELETE | `/api/sync/{id}` | `sync:write` | delete the job and its saved log |

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
until [ "$(curl -s "$BASE/sync/$JOB" -H "X-API-Key: $KEY" | jq -r .status)" != "running" ]; do
  sleep 10
done
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
