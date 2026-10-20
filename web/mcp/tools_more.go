package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools in this file are each one API call with nothing to decide on the
// way: the arguments become the request and the answer goes back unchanged.
// The ones in tools.go do more — polling, merging options, guarding deletes.

type apiReq struct {
	method, path string
	query        url.Values
	body         any
}

// apiTool registers a tool whose whole job is the request build returns.
func apiTool[In any](s *mcp.Server, t *tools, tool *mcp.Tool, build func(In) (apiReq, error)) {
	mcp.AddTool(s, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		req, err := build(in)
		if err != nil {
			return nil, nil, err
		}
		var out json.RawMessage
		if err := t.api.do(ctx, req.method, req.path, req.query, req.body, &out); err != nil {
			return nil, nil, err
		}
		if len(out) == 0 {
			out = json.RawMessage(`{"status":"ok"}`)
		}
		return result(out)
	})
}

// bodyOf turns an input struct into a request body without the fields that
// belong in the path. Optional fields are pointers with omitempty, so only
// what the model set is sent, and the API keeps the rest as it was.
func bodyOf(in any, drop ...string) (map[string]any, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for _, k := range drop {
		delete(m, k)
	}
	return m, nil
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

var (
	readOnly    = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}
	writes      = &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)}
	destructive = &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(true)}
)

// batchDeletePat finds delete options in a batch CSV's per-row options column.
var batchDeletePat = regexp.MustCompile(`--(delete1|delete2|delete2duplicates|expunge1|expunge2)\b`)

// ------------------------------------------------------------------ inputs

type profileIDInput struct {
	ProfileID int64 `json:"profile_id"`
}

type jobIDInput struct {
	JobID int64 `json:"job_id"`
}

type batchIDInput struct {
	BatchID int64 `json:"batch_id"`
}

type scheduleIDInput struct {
	ScheduleID int64 `json:"schedule_id"`
}

type keyIDInput struct {
	KeyID int64 `json:"key_id"`
}

type webhookIDInput struct {
	WebhookID int64 `json:"webhook_id"`
}

// partialMailbox is a mailbox where every field is optional: on an update,
// what is left out stays as saved, including the password.
type partialMailbox struct {
	Host     *string `json:"host,omitempty"`
	User     *string `json:"user,omitempty"`
	Password *string `json:"password,omitempty" jsonschema:"omit to keep the saved password"`
	SSL      *bool   `json:"ssl,omitempty"`
}

type updateProfileInput struct {
	ProfileID    int64           `json:"profile_id"`
	Name         *string         `json:"name,omitempty"`
	Source       *partialMailbox `json:"source,omitempty"`
	Destination  *partialMailbox `json:"destination,omitempty"`
	ExtraOptions map[string]any  `json:"extra_options,omitempty" jsonschema:"replaces the saved imapsync options"`
}

type createBatchInput struct {
	CSVText  string `json:"csv_text" jsonschema:"one mailbox per line: host1;user1;password1;host2;user2;password2[;imapsync options]. Lines starting with # are comments"`
	DryRun   *bool  `json:"dry_run,omitempty" jsonschema:"run every row as a dry run (default false)"`
	SSL1     *bool  `json:"ssl1,omitempty" jsonschema:"SSL to the sources (default true)"`
	SSL2     *bool  `json:"ssl2,omitempty" jsonschema:"SSL to the destinations (default true)"`
	Automap  *bool  `json:"automap,omitempty" jsonschema:"map folder names between servers (default true)"`
	UseCache *bool  `json:"usecache,omitempty" jsonschema:"default true"`
}

type createScheduleInput struct {
	ProfileID      int64  `json:"profile_id"`
	CronExpression string `json:"cron_expression" jsonschema:"five-field crontab in the server's time zone (UTC+7), e.g. \"0 3 * * *\" for 03:00 daily"`
	IsActive       *bool  `json:"is_active,omitempty" jsonschema:"default true"`
}

type updateScheduleInput struct {
	ScheduleID     int64   `json:"schedule_id"`
	CronExpression *string `json:"cron_expression,omitempty"`
	IsActive       *bool   `json:"is_active,omitempty"`
}

type purgeInput struct {
	OlderThanDays int `json:"older_than_days" jsonschema:"delete finished jobs older than this many days; 0 deletes every finished job"`
}

type retentionInput struct {
	Enabled  bool `json:"enabled"`
	KeepDays int  `json:"keep_days" jsonschema:"1 to 3650"`
}

type notificationsInput struct {
	SMTPHost        *string `json:"smtp_host,omitempty"`
	SMTPPort        *int    `json:"smtp_port,omitempty"`
	SMTPUser        *string `json:"smtp_user,omitempty"`
	SMTPPassword    *string `json:"smtp_password,omitempty" jsonschema:"omit to keep the saved password"`
	FromEmail       *string `json:"from_email,omitempty"`
	ToEmail         *string `json:"to_email,omitempty"`
	NotifyOnSuccess *bool   `json:"notify_on_success,omitempty"`
	NotifyOnFailure *bool   `json:"notify_on_failure,omitempty"`
}

type telegramInput struct {
	Enabled          *bool   `json:"enabled,omitempty"`
	BotToken         *string `json:"bot_token,omitempty" jsonschema:"omit to keep the saved token"`
	AllowedChatIDs   *string `json:"allowed_chat_ids,omitempty" jsonschema:"comma-separated Telegram chat ids allowed to use the bot"`
	UserID           *int64  `json:"user_id,omitempty" jsonschema:"the web UI user whose jobs the bot acts on"`
	NotifyOnSuccess  *bool   `json:"notify_on_success,omitempty"`
	NotifyOnFailure  *bool   `json:"notify_on_failure,omitempty"`
	NotifyOnProgress *bool   `json:"notify_on_progress,omitempty"`
	ProgressStep     *int    `json:"progress_step,omitempty" jsonschema:"percent between progress messages"`
}

type createKeyInput struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes" jsonschema:"see list_api_key_scopes; admin implies all"`
	RateLimit     int      `json:"rate_limit_per_minute,omitempty" jsonschema:"default 60"`
	ExpiresInDays int      `json:"expires_in_days,omitempty" jsonschema:"0 or omitted: never expires"`
}

type updateKeyInput struct {
	KeyID     int64    `json:"key_id"`
	Name      *string  `json:"name,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	RateLimit *int     `json:"rate_limit_per_minute,omitempty"`
}

type createWebhookInput struct {
	URL      string   `json:"url" jsonschema:"absolute http(s) URL that receives a JSON POST per event"`
	Events   []string `json:"events,omitempty" jsonschema:"see list_webhook_events; omit or [\"*\"] for all"`
	IsActive *bool    `json:"is_active,omitempty"`
}

type updateWebhookInput struct {
	WebhookID int64    `json:"webhook_id"`
	URL       *string  `json:"url,omitempty"`
	Events    []string `json:"events,omitempty"`
	IsActive  *bool    `json:"is_active,omitempty"`
}

// ---------------------------------------------------------------- register

func registerMore(s *mcp.Server, t *tools) {
	// ------------------------------------------------------------ account
	apiTool(s, t, &mcp.Tool{Name: "whoami",
		Description: "The web UI account this server's API key belongs to.", Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/auth/me"}, nil })

	// ----------------------------------------------------------- profiles
	apiTool(s, t, &mcp.Tool{Name: "get_profile",
		Description: "One saved profile in full, including its saved imapsync options. Never its passwords.",
		Annotations: readOnly},
		func(in profileIDInput) (apiReq, error) {
			return apiReq{method: "GET", path: "/profiles/" + id(in.ProfileID)}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "update_profile",
		Description: "Change a saved profile. Only what is given changes; an omitted password keeps the saved one.",
		Annotations: writes},
		func(in updateProfileInput) (apiReq, error) {
			if err := t.refuseDeleting(in.ExtraOptions, false); err != nil {
				return apiReq{}, err
			}
			body := map[string]any{}
			if in.Name != nil {
				body["name"] = *in.Name
			}
			for n, m := range map[string]*partialMailbox{"1": in.Source, "2": in.Destination} {
				if m == nil {
					continue
				}
				if m.Host != nil {
					body["host"+n] = *m.Host
				}
				if m.User != nil {
					body["user"+n] = *m.User
				}
				if m.Password != nil {
					body["password"+n] = *m.Password
				}
				if m.SSL != nil {
					body["ssl"+n] = *m.SSL
				}
			}
			if in.ExtraOptions != nil {
				body["extra_options"] = in.ExtraOptions
			}
			return apiReq{method: "PUT", path: "/profiles/" + id(in.ProfileID), body: body}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "delete_profile",
		Description: "Delete a saved profile. Past jobs run from it stay in the history.",
		Annotations: destructive},
		func(in profileIDInput) (apiReq, error) {
			return apiReq{method: "DELETE", path: "/profiles/" + id(in.ProfileID)}, nil
		})

	// --------------------------------------------------------------- jobs
	apiTool(s, t, &mcp.Tool{Name: "delete_job",
		Description: "Delete a finished job and its saved log from the history. A running job must be aborted first.",
		Annotations: destructive},
		func(in jobIDInput) (apiReq, error) {
			return apiReq{method: "DELETE", path: "/sync/" + id(in.JobID)}, nil
		})

	// ------------------------------------------------------------ batches
	apiTool(s, t, &mcp.Tool{Name: "create_batch",
		Description: "Prepare a batch of many mailbox pairs from CSV text. Nothing runs until " +
			"start_batch. Rows run one at a time, each as an ordinary job. The CSV carries " +
			"passwords, so they pass through this conversation; they are encrypted on the " +
			"server and erased when the batch finishes.",
		Annotations: writes},
		func(in createBatchInput) (apiReq, error) {
			dry := in.DryRun != nil && *in.DryRun
			if !t.allowDelete && !dry {
				if m := batchDeletePat.FindStringSubmatch(in.CSVText); m != nil {
					return apiReq{}, fmt.Errorf("refused: a row uses --%s, which deletes mail. This MCP "+
						"server only allows it when started with IMAPSYNC_MCP_ALLOW_DELETE=1; "+
						"a dry run is allowed", m[1])
				}
			}
			q := url.Values{}
			for name, v := range map[string]*bool{"dry_run": in.DryRun, "ssl1": in.SSL1,
				"ssl2": in.SSL2, "automap": in.Automap, "usecache": in.UseCache} {
				if v != nil {
					q.Set(name, strconv.FormatBool(*v))
				}
			}
			return apiReq{method: "POST", path: "/batch/upload-text", query: q,
				body: map[string]any{"csv_text": in.CSVText}}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "start_batch",
		Description: "Start running a prepared batch. Follow it with get_batch.", Annotations: writes},
		func(in batchIDInput) (apiReq, error) {
			return apiReq{method: "POST", path: "/batch/" + id(in.BatchID) + "/start"}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "get_batch",
		Description: "A batch's progress: total_rows, completed_rows, failed_rows and status.",
		Annotations: readOnly},
		func(in batchIDInput) (apiReq, error) {
			return apiReq{method: "GET", path: "/batch/" + id(in.BatchID)}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "abort_batch",
		Description: "Stop a running batch: the mailbox in flight is aborted and the remaining rows are skipped.",
		Annotations: destructive},
		func(in batchIDInput) (apiReq, error) {
			return apiReq{method: "POST", path: "/batch/" + id(in.BatchID) + "/abort"}, nil
		})

	// ---------------------------------------------------------- schedules
	apiTool(s, t, &mcp.Tool{Name: "list_schedules",
		Description: "Scheduled syncs: which profile runs on which cron expression.", Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/schedules"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "create_schedule",
		Description: "Run a profile automatically on a cron schedule.", Annotations: writes},
		func(in createScheduleInput) (apiReq, error) {
			body, err := bodyOf(in)
			return apiReq{method: "POST", path: "/schedules", body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "update_schedule",
		Description: "Change a schedule's cron expression, or pause and resume it with is_active.",
		Annotations: writes},
		func(in updateScheduleInput) (apiReq, error) {
			body, err := bodyOf(in, "schedule_id")
			return apiReq{method: "PUT", path: "/schedules/" + id(in.ScheduleID), body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "delete_schedule",
		Description: "Delete a schedule. The profile and its past jobs are kept.", Annotations: destructive},
		func(in scheduleIDInput) (apiReq, error) {
			return apiReq{method: "DELETE", path: "/schedules/" + id(in.ScheduleID)}, nil
		})

	// ------------------------------------------------------------ history
	apiTool(s, t, &mcp.Tool{Name: "purge_history",
		Description: "Delete finished jobs and their logs older than a number of days. Running " +
			"jobs are never touched. Cannot be undone.",
		Annotations: destructive},
		func(in purgeInput) (apiReq, error) {
			if in.OlderThanDays < 0 {
				return apiReq{}, fmt.Errorf("older_than_days cannot be negative")
			}
			return apiReq{method: "POST", path: "/history/purge",
				body: map[string]int{"older_than_days": in.OlderThanDays}}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "get_history_retention",
		Description: "The automatic history cleanup policy, applied nightly at 03:30, and when it last ran.",
		Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/history/retention"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "set_history_retention",
		Description: "Turn the nightly history cleanup on or off and set how many days of finished jobs it keeps.",
		Annotations: destructive},
		func(in retentionInput) (apiReq, error) {
			body, err := bodyOf(in)
			return apiReq{method: "PUT", path: "/history/retention", body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "run_history_retention",
		Description: "Apply the saved retention policy now instead of waiting for the night. Fails while it is off.",
		Annotations: destructive},
		func(noInput) (apiReq, error) { return apiReq{method: "POST", path: "/history/retention/run"}, nil })

	// ------------------------------------------------------ notifications
	apiTool(s, t, &mcp.Tool{Name: "get_email_notifications",
		Description: "Email notification settings (SMTP and when to send). Needs an admin key.",
		Annotations: readOnly},
		func(noInput) (apiReq, error) {
			return apiReq{method: "GET", path: "/notifications/settings"}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "update_email_notifications",
		Description: "Change email notification settings. Only what is given changes. Needs an admin key.",
		Annotations: writes},
		func(in notificationsInput) (apiReq, error) {
			body, err := bodyOf(in)
			return apiReq{method: "PUT", path: "/notifications/settings", body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "test_email_notifications",
		Description: "Send a test email with the saved SMTP settings. Needs an admin key.", Annotations: writes},
		func(noInput) (apiReq, error) { return apiReq{method: "POST", path: "/notifications/test"}, nil })

	// ----------------------------------------------------------- telegram
	apiTool(s, t, &mcp.Tool{Name: "get_telegram",
		Description: "Telegram bot settings; has_token says whether a token is saved. Needs an admin key.",
		Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/telegram/settings"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "update_telegram",
		Description: "Change the Telegram bot settings. Only what is given changes. Needs an admin key.",
		Annotations: writes},
		func(in telegramInput) (apiReq, error) {
			body, err := bodyOf(in)
			return apiReq{method: "PUT", path: "/telegram/settings", body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "telegram_status",
		Description: "Whether the Telegram bot is running. Needs an admin key.", Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/telegram/status"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "test_telegram",
		Description: "Send a test message to the allowed Telegram chats. Needs an admin key.", Annotations: writes},
		func(noInput) (apiReq, error) { return apiReq{method: "POST", path: "/telegram/test"}, nil })

	// ----------------------------------------------------------- api keys
	apiTool(s, t, &mcp.Tool{Name: "list_api_keys",
		Description: "API keys: name, prefix, scopes, rate limit, last use. Never the keys themselves. Needs an admin key.",
		Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/api-keys"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "list_api_key_scopes",
		Description: "The scope names an API key can carry. Needs an admin key.", Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/api-keys/scopes"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "create_api_key",
		Description: "Issue a new API key. The key is in this answer only and cannot be shown " +
			"again — hand it to the user to store. Needs an admin key.",
		Annotations: writes},
		func(in createKeyInput) (apiReq, error) {
			body, err := bodyOf(in)
			return apiReq{method: "POST", path: "/api-keys", body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "update_api_key",
		Description: "Change a key's name, scopes or rate limit; the key itself keeps working. Needs an admin key.",
		Annotations: writes},
		func(in updateKeyInput) (apiReq, error) {
			body, err := bodyOf(in, "key_id")
			return apiReq{method: "PUT", path: "/api-keys/" + id(in.KeyID), body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "revoke_api_key",
		Description: "Stop a key working, keeping its record. Revoking the key this server uses cuts it off. Needs an admin key.",
		Annotations: destructive},
		func(in keyIDInput) (apiReq, error) {
			return apiReq{method: "POST", path: "/api-keys/" + id(in.KeyID) + "/revoke"}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "delete_api_key",
		Description: "Delete a key entirely; whatever uses it gets 401 from now on. Needs an admin key.",
		Annotations: destructive},
		func(in keyIDInput) (apiReq, error) {
			return apiReq{method: "DELETE", path: "/api-keys/" + id(in.KeyID)}, nil
		})

	// ----------------------------------------------------------- webhooks
	apiTool(s, t, &mcp.Tool{Name: "list_webhooks",
		Description: "Registered webhooks and the result of their last delivery. Needs an admin key.",
		Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/webhooks"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "list_webhook_events",
		Description: "The event names a webhook can subscribe to. Needs an admin key.", Annotations: readOnly},
		func(noInput) (apiReq, error) { return apiReq{method: "GET", path: "/webhooks/events"}, nil })

	apiTool(s, t, &mcp.Tool{Name: "create_webhook",
		Description: "Register a URL to be called on job events. The signing secret is in this " +
			"answer only — hand it to the user. Needs an admin key.",
		Annotations: writes},
		func(in createWebhookInput) (apiReq, error) {
			body, err := bodyOf(in)
			return apiReq{method: "POST", path: "/webhooks", body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "update_webhook",
		Description: "Change a webhook's URL or events, or disable and enable it. Needs an admin key.",
		Annotations: writes},
		func(in updateWebhookInput) (apiReq, error) {
			body, err := bodyOf(in, "webhook_id")
			return apiReq{method: "PUT", path: "/webhooks/" + id(in.WebhookID), body: body}, err
		})

	apiTool(s, t, &mcp.Tool{Name: "test_webhook",
		Description: "Send a synthetic delivery to a webhook and report what it answered. Needs an admin key.",
		Annotations: writes},
		func(in webhookIDInput) (apiReq, error) {
			return apiReq{method: "POST", path: "/webhooks/" + id(in.WebhookID) + "/test"}, nil
		})

	apiTool(s, t, &mcp.Tool{Name: "delete_webhook",
		Description: "Remove a webhook. Needs an admin key.", Annotations: destructive},
		func(in webhookIDInput) (apiReq, error) {
			return apiReq{method: "DELETE", path: "/webhooks/" + id(in.WebhookID)}, nil
		})
}
