# imapsync-mcp

An [MCP](https://modelcontextprotocol.io) server for the imapsync web UI. It
runs on your machine over stdio and drives a deployed instance through its
[HTTP API](../../external-api-syncmail.md), so Claude (or any MCP client) can
list profiles, start and watch syncs, and read their logs.

## Build

```bash
make build        # ./imapsync-mcp for this machine
make dist         # dist/ binaries for macOS, Linux and Windows
```

## Get a key

In the web UI, **API Access → Create key**. Give it only what the model needs:

| Scope | Needed for |
|---|---|
| `profiles:read` | `list_profiles`, `browse_folders`, and the safety check in `start_sync` |
| `profiles:write` | `create_profile` |
| `sync:read` | `get_job`, `get_job_log`, `list_running_jobs`, `list_history`, `history_stats` |
| `sync:write` | `start_sync`, `abort_job` |

Leave `admin` off. A read-only assistant needs just `profiles:read` and
`sync:read`; the write tools then answer 403. Scopes can be changed later
with **Edit** without replacing the key.

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

| Tool | Does |
|---|---|
| `list_profiles` | saved mailbox pairs; never their passwords |
| `create_profile` | save a source/destination pair given by hand, for later syncs |
| `browse_folders` | folders of a mailbox — a profile's source, or one given by hand — with counts and sizes |
| `start_sync` | start a sync for a profile or for two mailboxes given by hand; optionally a dry run, some folders, extra options |
| `get_job` | a job's status and counters; `wait_seconds` blocks until it stops running |
| `get_job_log` | the last lines of a job's log, or a page from an offset |
| `list_running_jobs` | jobs running now |
| `abort_job` | stop a running job |
| `list_history` | past jobs, filtered by status or account |
| `history_stats` | totals across all jobs |

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
  A dry run is always allowed — it writes nothing. `create_profile` will not
  save these options either.
- **Administer the server.** Keys, webhooks, notification settings, history
  purges and job deletion are left to the web UI.

## Waiting on slow things

Two operations outlast a typical tool-call timeout, and both are split so no
single call has to:

- `browse_folders` waits up to 45 seconds, then returns `status: "running"`.
  Calling it again with the same profile joins the listing already running.
- `get_job` with `wait_seconds` holds for at most 120 seconds; the model calls
  again to keep waiting. Each wait is one request against the key's rate limit.
