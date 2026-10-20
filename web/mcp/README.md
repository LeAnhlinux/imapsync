# imapsync-mcp

An [MCP](https://modelcontextprotocol.io) server for the imapsync web UI. It
runs on your machine over stdio and drives a deployed instance through its
[HTTP API](../../external-api-syncmail.md), so Claude (or any MCP client) can
list profiles, start and watch syncs, and read their logs.

Hướng dẫn cài đặt và sử dụng bằng tiếng Việt: [HUONG_DAN.md](HUONG_DAN.md).

## Build

```bash
make build        # ./imapsync-mcp for this machine
make dist         # dist/ binaries for macOS, Linux and Windows
```

## Get a key

In the web UI, **API Access → Create key**. Every API endpoint has a tool, and
the key's scopes decide which of them work — the rest answer 403:

| Scope | Tools |
|---|---|
| `profiles:read` | `list_profiles`, `get_profile`, `browse_folders`; also the delete check in `start_sync` by profile |
| `profiles:write` | `create_profile`, `update_profile`, `delete_profile` |
| `sync:read` | `get_job`, `get_job_log`, `list_running_jobs`, `list_history`, `history_stats`, `get_batch`, `get_history_retention` |
| `sync:write` | `start_sync`, `abort_job`, `delete_job`, `purge_history`, `set_history_retention`, `run_history_retention` |
| `batch:write` | `create_batch`, `start_batch`, `abort_batch` |
| `schedules:read` / `schedules:write` | `list_schedules` / `create_schedule`, `update_schedule`, `delete_schedule` |
| `admin` | everything, plus email and Telegram notifications, API keys and webhooks |

For day-to-day migrations, `profiles:read`, `profiles:write`, `sync:read`,
`sync:write` is enough. Give `admin` only to an assistant you want managing
the server itself — it can issue more keys. Scopes can be changed later with
**Edit** without replacing the key.

## Configure

| Variable | |
|---|---|
| `IMAPSYNC_URL` | the site, e.g. `https://sync.example.com` (with or without `/api`) |
| `IMAPSYNC_API_KEY` | the `isk_…` key |
| `IMAPSYNC_MCP_ALLOW_DELETE` | `1` to allow `delete1`, `delete2`, `delete2duplicates`, `expunge1`, `expunge2` |

**Claude Code:**

```bash
claude mcp add imapsync \
  -e IMAPSYNC_URL=https://sync.example.com \
  -e IMAPSYNC_API_KEY=isk_... \
  -- /path/to/imapsync-mcp
```

**Claude Desktop** (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "imapsync": {
      "command": "/path/to/imapsync-mcp",
      "env": {
        "IMAPSYNC_URL": "https://sync.example.com",
        "IMAPSYNC_API_KEY": "isk_..."
      }
    }
  }
}
```

## Tools

46 tools, one or more per [API endpoint](../../external-api-syncmail.md#appendix-every-endpoint).

| Area | Tools |
|---|---|
| Account | `whoami` |
| Profiles | `list_profiles`, `get_profile`, `create_profile`, `update_profile`, `delete_profile` |
| Folders | `browse_folders` — a profile's source, or a mailbox given by hand |
| Syncs | `start_sync`, `get_job` (with `wait_seconds`), `get_job_log` (tail or pages), `list_running_jobs`, `abort_job`, `delete_job` |
| Batches | `create_batch` (CSV text), `start_batch`, `get_batch`, `abort_batch` |
| Schedules | `list_schedules`, `create_schedule`, `update_schedule`, `delete_schedule` |
| History | `list_history`, `history_stats`, `purge_history`, `get_history_retention`, `set_history_retention`, `run_history_retention` |
| Email alerts | `get_email_notifications`, `update_email_notifications`, `test_email_notifications` |
| Telegram | `get_telegram`, `update_telegram`, `telegram_status`, `test_telegram` |
| API keys | `list_api_keys`, `list_api_key_scopes`, `create_api_key`, `update_api_key`, `revoke_api_key`, `delete_api_key` |
| Webhooks | `list_webhooks`, `list_webhook_events`, `create_webhook`, `update_webhook`, `test_webhook`, `delete_webhook` |

Tools that delete or stop something are marked destructive, so clients that
ask before acting (Claude Code does) ask for those.

The two API endpoints without a tool are `POST /batch/upload`, the multipart
twin of `create_batch`, and the job WebSocket, which `get_job` and
`get_job_log` replace.

## Two ways to name the mailboxes

**By profile** — "sync Customer A". The passwords stay encrypted on the
server and never pass through the model.

**By hand** — "copy mail from a@old.com (mail.old.com, password …) to
b@new.com (imap.new.com, password …)". `start_sync`, `browse_folders` and
`create_profile` all take `source` / `destination` (or `mailbox`) with
`host`, `user`, `password`, and optionally `ssl` (default on) and `port`.

Given by hand, **the passwords are part of the conversation**: they reach the
model provider and stay in the client's transcript like any other message.
Prefer app passwords, which can be revoked after the migration, and save a
pair you will sync again with `create_profile` so later runs can name it.

## What it will not do

- **Delete mail** unless `IMAPSYNC_MCP_ALLOW_DELETE=1`. The check covers the
  profile's saved options as well as the call's, since both apply to the run.
  A dry run is always allowed — it writes nothing. `create_profile`,
  `update_profile` and `create_batch` will not save or queue them either.

## Waiting on slow things

Two operations outlast a typical tool-call timeout, and both are split so no
single call has to:

- `browse_folders` waits up to 45 seconds, then returns `status: "running"`.
  Calling it again with the same profile joins the listing already running.
- `get_job` with `wait_seconds` holds for at most 120 seconds; the model calls
  again to keep waiting. Each wait is one request against the key's rate limit.

## Testing

`go test ./...` runs against a fake API. `TestEndToEnd` drives every tool
against a real server and fails if a tool is added without being covered;
point it at a throwaway instance whose `IMAPSYNC_PATH` is a stub, with an
admin-scoped key:

```bash
IMAPSYNC_E2E_URL=http://127.0.0.1:18080 IMAPSYNC_E2E_KEY=isk_... go test -run EndToEnd -v
```
