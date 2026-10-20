package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// destructiveOptions are the imapsync options that delete mail. The API
// accepts them — deleting from the source after a migration is legitimate —
// but a model should not reach for them on its own, so they are refused unless
// whoever configured this server opted in.
var destructiveOptions = []string{"delete1", "delete2", "delete2duplicates", "expunge1", "expunge2"}

// browsePollEvery and browseHold shape one browse_folders call: poll the
// listing for up to browseHold, then hand back "running" rather than outlast
// the client's tool-call timeout.
var (
	browsePollEvery = 3 * time.Second
	browseHold      = 45 * time.Second
)

type tools struct {
	api         *Client
	allowDelete bool
}

func ptr[T any](v T) *T { return &v }

// result wraps an API answer as the tool's output. The SDK renders it as JSON
// text for the model.
func result(v any) (*mcp.CallToolResult, any, error) {
	return nil, v, nil
}

func register(s *mcp.Server, t *tools) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_profiles",
		Description: "List the saved mailbox pairs (profiles). A sync is started by profile, " +
			"so this is usually the first call. Passwords are never returned.",
		Annotations: readOnly,
	}, t.listProfiles)

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_profile",
		Description: "Save a mailbox pair — source and destination connection details — as a " +
			"profile, so later syncs can name it by profile_id. Passwords are stored encrypted " +
			"on the server and never returned.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, t.createProfile)

	mcp.AddTool(s, &mcp.Tool{
		Name: "browse_folders",
		Description: "List a mailbox's folders with message counts and sizes, to choose which " +
			"folders to sync. Give profile_id to list that profile's SOURCE mailbox, or mailbox " +
			"with connection details for any mailbox. Large mailboxes take minutes: if the answer " +
			"has status \"running\", call again with the same arguments — it joins the listing " +
			"already in progress rather than starting another.",
		Annotations: readOnly,
	}, t.browseFolders)

	mcp.AddTool(s, &mcp.Tool{
		Name: "start_sync",
		Description: "Start copying mail from a source mailbox to a destination mailbox. Name a " +
			"saved pair with profile_id, or give source and destination connection details " +
			"directly. Returns the job; follow it with get_job (wait_seconds) and get_job_log. " +
			"Use dry_run first on an unfamiliar pair: it connects and reports what would be " +
			"copied without writing anything. Options that delete mail " +
			"(" + strings.Join(destructiveOptions, ", ") + ") are refused unless the server was " +
			"started with IMAPSYNC_MCP_ALLOW_DELETE=1.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, t.startSync)

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_job",
		Description: "Get a sync job's status and counters. With wait_seconds (up to 120) the call " +
			"returns as soon as the job stops running, or when the time is up with it still " +
			"running — call again to keep waiting. Status is one of running, completed, failed, " +
			"aborted, interrupted.",
		Annotations: readOnly,
	}, t.getJob)

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_job_log",
		Description: "Read part of a job's imapsync log. By default the last 100 lines. To follow " +
			"a running job, pass offset = the next_offset from the previous call; an empty log " +
			"means nothing new yet. Logs contain message subjects — quote them only as needed.",
		Annotations: readOnly,
	}, t.getJobLog)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_running_jobs",
		Description: "List the sync jobs running right now.",
		Annotations: readOnly,
	}, t.listRunning)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "abort_job",
		Description: "Stop a running sync job. Mail already copied stays copied.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(true)},
	}, t.abortJob)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_history",
		Description: "List past sync jobs, newest first, optionally filtered by status or by a search on the account names.",
		Annotations: readOnly,
	}, t.listHistory)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "history_stats",
		Description: "Totals across all sync jobs: how many ran, succeeded and failed, and how much mail moved.",
		Annotations: readOnly,
	}, t.historyStats)

	registerMore(s, t)
}

// ------------------------------------------------------------------ profiles

type noInput struct{}

type profileSummary struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Source       string         `json:"source"`
	Destination  string         `json:"destination"`
	ExtraOptions map[string]any `json:"extra_options,omitempty"`
}

type apiProfile struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Host1        string         `json:"host1"`
	User1        string         `json:"user1"`
	Host2        string         `json:"host2"`
	User2        string         `json:"user2"`
	ExtraOptions map[string]any `json:"extra_options"`
}

func (t *tools) listProfiles(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	var ps []apiProfile
	if err := t.api.do(ctx, "GET", "/profiles", nil, nil, &ps); err != nil {
		return nil, nil, err
	}
	out := make([]profileSummary, 0, len(ps))
	for _, p := range ps {
		out = append(out, profileSummary{
			ID: p.ID, Name: p.Name,
			Source:       p.User1 + " @ " + p.Host1,
			Destination:  p.User2 + " @ " + p.Host2,
			ExtraOptions: p.ExtraOptions,
		})
	}
	return result(out)
}

// mailbox is one side of a migration, as the model gives it.
type mailbox struct {
	Host     string `json:"host" jsonschema:"IMAP server, e.g. imap.gmail.com or mail.example.com"`
	User     string `json:"user" jsonschema:"login, usually the full email address"`
	Password string `json:"password" jsonschema:"password or app password"`
	SSL      *bool  `json:"ssl,omitempty" jsonschema:"connect with SSL/TLS (default true, port 993)"`
	Port     int    `json:"port,omitempty" jsonschema:"only if the server uses a non-standard port"`
}

func (m *mailbox) check(side string) error {
	if m == nil {
		return fmt.Errorf("%s mailbox is missing", side)
	}
	m.Host, m.User = strings.TrimSpace(m.Host), strings.TrimSpace(m.User)
	if m.Host == "" || m.User == "" || m.Password == "" {
		return fmt.Errorf("%s mailbox needs host, user and password", side)
	}
	return nil
}

func (m *mailbox) ssl() bool { return m.SSL == nil || *m.SSL }

// pairBody is the API's flat host1/user1/... form of a source and destination,
// with any non-standard ports carried as the port1/port2 imapsync options.
func pairBody(src, dst *mailbox, extra map[string]any) map[string]any {
	opts := map[string]any{}
	for k, v := range extra {
		opts[k] = v
	}
	if src.Port > 0 {
		opts["port1"] = src.Port
	}
	if dst.Port > 0 {
		opts["port2"] = dst.Port
	}
	body := map[string]any{
		"host1": src.Host, "user1": src.User, "password1": src.Password, "ssl1": src.ssl(),
		"host2": dst.Host, "user2": dst.User, "password2": dst.Password, "ssl2": dst.ssl(),
	}
	if len(opts) > 0 {
		body["extra_options"] = opts
	}
	return body
}

type createProfileInput struct {
	Name         string         `json:"name" jsonschema:"a label to tell this pair apart, e.g. the customer or account"`
	Source       *mailbox       `json:"source" jsonschema:"the mailbox mail is copied from"`
	Destination  *mailbox       `json:"destination" jsonschema:"the mailbox mail is copied to"`
	ExtraOptions map[string]any `json:"extra_options,omitempty" jsonschema:"imapsync options saved with the profile, e.g. {\"automap\": true}"`
}

func (t *tools) createProfile(ctx context.Context, _ *mcp.CallToolRequest, in createProfileInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if err := in.Source.check("source"); err != nil {
		return nil, nil, err
	}
	if err := in.Destination.check("destination"); err != nil {
		return nil, nil, err
	}
	// Saved delete options would apply to every later run of the profile.
	if err := t.refuseDeleting(in.ExtraOptions, false); err != nil {
		return nil, nil, err
	}
	body := pairBody(in.Source, in.Destination, in.ExtraOptions)
	body["name"] = strings.TrimSpace(in.Name)
	var p apiProfile
	if err := t.api.do(ctx, "POST", "/profiles", nil, body, &p); err != nil {
		return nil, nil, err
	}
	return result(profileSummary{
		ID: p.ID, Name: p.Name,
		Source:       p.User1 + " @ " + p.Host1,
		Destination:  p.User2 + " @ " + p.Host2,
		ExtraOptions: p.ExtraOptions,
	})
}

// ------------------------------------------------------------------- folders

type browseInput struct {
	ProfileID int64    `json:"profile_id,omitempty" jsonschema:"the profile whose source mailbox to list"`
	Mailbox   *mailbox `json:"mailbox,omitempty" jsonschema:"a mailbox to list, instead of profile_id"`
}

type browseTask struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"`
	Folders []json.RawMessage `json:"folders,omitempty"`
	Error   string            `json:"error,omitempty"`
}

func (t *tools) browseFolders(ctx context.Context, _ *mcp.CallToolRequest, in browseInput) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"async": true}
	switch {
	case in.ProfileID > 0:
		body["profile_id"] = in.ProfileID
	case in.Mailbox != nil:
		if err := in.Mailbox.check("the"); err != nil {
			return nil, nil, err
		}
		body["host"], body["user"], body["password"], body["ssl"] =
			in.Mailbox.Host, in.Mailbox.User, in.Mailbox.Password, in.Mailbox.ssl()
	default:
		return nil, nil, fmt.Errorf("give profile_id or mailbox")
	}
	var task browseTask
	if err := t.api.do(ctx, "POST", "/folders/browse", nil, body, &task); err != nil {
		return nil, nil, err
	}

	deadline := time.Now().Add(browseHold)
	for task.Status == "running" && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(browsePollEvery):
		}
		if err := t.api.do(ctx, "GET", "/folders/browse/"+url.PathEscape(task.ID), nil, nil, &task); err != nil {
			return nil, nil, err
		}
	}

	switch task.Status {
	case "failed":
		return nil, nil, fmt.Errorf("listing folders failed: %s", task.Error)
	case "running":
		return result(map[string]string{
			"status": "running",
			"note":   "Still listing — large mailboxes take minutes. Call browse_folders again with the same arguments to keep waiting.",
		})
	}
	return result(map[string]any{"status": "done", "folders": task.Folders})
}

// ---------------------------------------------------------------------- sync

type startInput struct {
	ProfileID    int64          `json:"profile_id,omitempty" jsonschema:"a saved pair; or give source and destination instead"`
	Source       *mailbox       `json:"source,omitempty" jsonschema:"the mailbox mail is copied from, when not using profile_id"`
	Destination  *mailbox       `json:"destination,omitempty" jsonschema:"the mailbox mail is copied to, when not using profile_id"`
	Folders      []string       `json:"folders,omitempty" jsonschema:"folder names to sync; omit to sync every folder"`
	DryRun       bool           `json:"dry_run,omitempty" jsonschema:"connect and report without writing anything"`
	ExtraOptions map[string]any `json:"extra_options,omitempty" jsonschema:"imapsync options for this run, e.g. {\"automap\": true, \"maxage\": 30}; merged over the profile's saved options"`
}

func (t *tools) startSync(ctx context.Context, _ *mcp.CallToolRequest, in startInput) (*mcp.CallToolResult, any, error) {
	var body map[string]any
	switch {
	case in.ProfileID > 0:
		opts := in.ExtraOptions
		if !t.allowDelete && !in.DryRun {
			// The profile's saved options apply to the run too, so a delete
			// option stored there would slip past a check of the arguments alone.
			var p apiProfile
			if err := t.api.do(ctx, "GET", "/profiles/"+strconv.FormatInt(in.ProfileID, 10), nil, nil, &p); err != nil {
				return nil, nil, err
			}
			opts = map[string]any{}
			for k, v := range p.ExtraOptions {
				opts[k] = v
			}
			for k, v := range in.ExtraOptions {
				opts[k] = v
			}
		}
		if err := t.refuseDeleting(opts, in.DryRun); err != nil {
			return nil, nil, err
		}
		body = map[string]any{"profile_id": in.ProfileID}
		if len(in.ExtraOptions) > 0 {
			body["extra_options"] = in.ExtraOptions
		}
	case in.Source != nil || in.Destination != nil:
		if err := in.Source.check("source"); err != nil {
			return nil, nil, err
		}
		if err := in.Destination.check("destination"); err != nil {
			return nil, nil, err
		}
		if err := t.refuseDeleting(in.ExtraOptions, in.DryRun); err != nil {
			return nil, nil, err
		}
		body = pairBody(in.Source, in.Destination, in.ExtraOptions)
	default:
		return nil, nil, fmt.Errorf("give profile_id, or source and destination")
	}

	body["dry_run"] = in.DryRun
	if len(in.Folders) > 0 {
		body["folders"] = in.Folders
	}
	var job json.RawMessage
	if err := t.api.do(ctx, "POST", "/sync/start", nil, body, &job); err != nil {
		return nil, nil, err
	}
	return result(job)
}

// refuseDeleting fails when opts would delete mail and this server was not
// configured to allow it. A dry run writes nothing, so it is always let through.
func (t *tools) refuseDeleting(opts map[string]any, dryRun bool) error {
	if t.allowDelete || dryRun {
		return nil
	}
	if found := deletingOptions(opts); len(found) > 0 {
		return fmt.Errorf("refused: %s would delete mail. This MCP server only runs "+
			"such syncs when started with IMAPSYNC_MCP_ALLOW_DELETE=1; a dry_run is allowed",
			strings.Join(found, ", "))
	}
	return nil
}

// deletingOptions returns the destructive options switched on in opts, sorted.
func deletingOptions(opts map[string]any) []string {
	var found []string
	for _, name := range destructiveOptions {
		v, ok := opts[name]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case nil:
			continue
		case bool:
			if !x {
				continue
			}
		case float64:
			if x == 0 {
				continue
			}
		case string:
			if s := strings.ToLower(strings.TrimSpace(x)); s == "" || s == "0" || s == "false" {
				continue
			}
		}
		found = append(found, name)
	}
	sort.Strings(found)
	return found
}

type jobInput struct {
	JobID       int64 `json:"job_id"`
	WaitSeconds int   `json:"wait_seconds,omitempty" jsonschema:"hold the call up to this many seconds (max 120) until the job stops running"`
}

func (t *tools) getJob(ctx context.Context, _ *mcp.CallToolRequest, in jobInput) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if in.WaitSeconds > 0 {
		q.Set("wait", strconv.Itoa(min(in.WaitSeconds, 120)))
	}
	var job json.RawMessage
	if err := t.api.do(ctx, "GET", "/sync/"+strconv.FormatInt(in.JobID, 10), q, nil, &job); err != nil {
		return nil, nil, err
	}
	return result(job)
}

type logInput struct {
	JobID  int64 `json:"job_id"`
	Tail   int   `json:"tail,omitempty" jsonschema:"how many of the last lines to return (default 100, max 5000); ignored when offset is given"`
	Offset *int  `json:"offset,omitempty" jsonschema:"byte offset to read from, normally next_offset from the previous call; returns up to 256 KB"`
}

func (t *tools) getJobLog(ctx context.Context, _ *mcp.CallToolRequest, in logInput) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if in.Offset != nil {
		q.Set("offset", strconv.Itoa(*in.Offset))
	} else {
		tail := in.Tail
		if tail <= 0 {
			tail = 100
		}
		q.Set("tail", strconv.Itoa(tail))
	}
	var page json.RawMessage
	if err := t.api.do(ctx, "GET", "/sync/"+strconv.FormatInt(in.JobID, 10)+"/log", q, nil, &page); err != nil {
		return nil, nil, err
	}
	return result(page)
}

func (t *tools) listRunning(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	var jobs json.RawMessage
	if err := t.api.do(ctx, "GET", "/sync/running", nil, nil, &jobs); err != nil {
		return nil, nil, err
	}
	return result(jobs)
}

type abortInput struct {
	JobID int64 `json:"job_id"`
}

func (t *tools) abortJob(ctx context.Context, _ *mcp.CallToolRequest, in abortInput) (*mcp.CallToolResult, any, error) {
	var out json.RawMessage
	if err := t.api.do(ctx, "POST", "/sync/"+strconv.FormatInt(in.JobID, 10)+"/abort", nil, nil, &out); err != nil {
		return nil, nil, err
	}
	return result(out)
}

// ------------------------------------------------------------------- history

type historyInput struct {
	Status  string `json:"status,omitempty" jsonschema:"only jobs with this status: completed, failed, aborted, interrupted or running"`
	Query   string `json:"query,omitempty" jsonschema:"search the source and destination account names"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"jobs per page, default 20, max 100"`
}

func (t *tools) listHistory(ctx context.Context, _ *mcp.CallToolRequest, in historyInput) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if in.Status != "" {
		q.Set("status", in.Status)
	}
	if in.Query != "" {
		q.Set("q", in.Query)
	}
	if in.Page > 0 {
		q.Set("page", strconv.Itoa(in.Page))
	}
	perPage := in.PerPage
	if perPage <= 0 {
		perPage = 20
	}
	q.Set("per_page", strconv.Itoa(min(perPage, 100)))
	var jobs json.RawMessage
	if err := t.api.do(ctx, "GET", "/history", q, nil, &jobs); err != nil {
		return nil, nil, err
	}
	return result(jobs)
}

func (t *tools) historyStats(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	var st json.RawMessage
	if err := t.api.do(ctx, "GET", "/history/stats", nil, nil, &st); err != nil {
		return nil, nil, err
	}
	return result(st)
}
