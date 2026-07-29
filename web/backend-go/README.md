# imapsync Web UI — Go backend

A rewrite of `web/backend` (FastAPI) as a single static binary. Same database,
same HTTP API, same frontend.

    go build -o imapsync-web ./cmd/server
    go build -o createadmin  ./cmd/createadmin

`CGO_ENABLED=0` works: the SQLite driver is pure Go, so the binary runs on any
Linux host without a Python runtime, a venv or system libraries.

## Compatibility with the Python backend

Verified against a database written by the Python backend:

- **Fernet** — `fernet-go` reads tokens written by Python's `cryptography`, and
  Python reads tokens written by Go. Stored IMAP passwords survive the switch
  with no re-encryption pass.
- **bcrypt** — existing password hashes verify unchanged.
- **Timestamps** — stored as naive UTC+7 in SQLAlchemy's
  `2006-01-02 15:04:05.000000` layout, and serialised to JSON without a
  timezone suffix. Go's default `time.Time` marshalling appends `Z`, which
  would make the browser shift every timestamp by its own offset; see
  `store.NaiveTime`.
- **Schema** — `CREATE TABLE IF NOT EXISTS` plus an idempotent column
  migration, matching what the Python side created.

## Differences from the Python backend

- **WAL is on.** Background goroutines write job rows while HTTP handlers read
  them; the default rollback journal turns that into "database is locked".
- **One process, any number of workers.** The live-job registry, the WebSocket
  subscribers and the scheduler are shared memory rather than per-process
  state, so the `--workers 1` constraint the Python deployment needed is gone.
- **`GET /api/history` sets `X-Total-Count`**, so pagination has a total.
- **Cron expressions are validated** when a schedule is saved, instead of
  failing silently at load time.

## Not ported yet

- The Telegram bot. The settings table, the API endpoints and the Settings-page
  form all work; `api.Bot` is the seam the bot plugs into, and `/api/telegram/status`
  reports `running: false` until it lands.
