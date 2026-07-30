package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/imapsync/web-backend/internal/api"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/services"
	"time"

	"github.com/imapsync/web-backend/internal/store"
)

func (b *Bot) dispatch(ctx context.Context, cfg config, msg *message) string {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Caption)
	}
	fields := strings.Fields(text)
	command := ""
	if len(fields) > 0 {
		// Telegram appends @botname to commands in groups.
		command = strings.ToLower(strings.SplitN(fields[0], "@", 2)[0])
	}

	if msg.Document != nil {
		if command != "" && command != "/batch" {
			return "I only accept files with the /batch caption. Got: " + command
		}
		return b.cmdBatchUpload(ctx, cfg, msg)
	}

	intArg := func() (int64, bool) {
		if len(fields) < 2 {
			return 0, false
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		return n, err == nil
	}

	switch command {
	case "", "/start", "/help":
		return helpText
	case "/profiles":
		return b.cmdProfiles(cfg)
	case "/jobs":
		return b.cmdJobs(cfg)
	case "/history":
		return b.cmdHistory(cfg)
	case "/batch":
		return "Send me a .csv file with /batch as its caption.\n\n" +
			"Format (semicolon separated):\nhost1;user1;password1;host2;user2;password2"
	case "/run":
		body := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
		return b.cmdRun(ctx, cfg, body, msg)
	case "/sync":
		id, ok := intArg()
		if !ok {
			return "Usage: /sync <profile_id>   (see /profiles)"
		}
		return b.cmdSync(cfg, id)
	case "/status":
		id, ok := intArg()
		if !ok {
			return "Usage: /status <job_id>"
		}
		return b.cmdStatus(cfg, id)
	case "/stop":
		id, ok := intArg()
		if !ok {
			return "Usage: /stop <job_id>"
		}
		return b.cmdStop(cfg, id)
	case "/batchgo":
		id, ok := intArg()
		if !ok {
			return "Usage: /batchgo <batch_id>"
		}
		return b.cmdBatchGo(cfg, id)
	}
	return fmt.Sprintf("Unknown command: %s\n\n%s", command, helpText)
}

func (b *Bot) cmdProfiles(cfg config) string {
	profiles, err := b.DB.ProfilesByUser(cfg.userID)
	if err != nil {
		return "Could not read the profiles: " + err.Error()
	}
	if len(profiles) == 0 {
		return "No profiles yet. Create one in the web UI first."
	}
	lines := []string{"Profiles:"}
	for _, p := range profiles {
		// user1 is usually already a full address, so don't glue the host on.
		lines = append(lines, fmt.Sprintf("  %d. %s\n     from: %s on %s\n     to:   %s on %s",
			p.ID, p.Name, p.User1, p.Host1, p.User2, p.Host2))
	}
	lines = append(lines, "\nStart one with: /sync <id>")
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdJobs(cfg config) string {
	jobs, err := b.DB.RunningJobs(cfg.userID)
	if err != nil {
		return "Could not read the jobs: " + err.Error()
	}
	if len(jobs) == 0 {
		return "Nothing running."
	}
	lines := []string{"Running:"}
	for _, j := range jobs {
		lines = append(lines, fmt.Sprintf("  #%d %s -> %s", j.ID, deref(j.SourceAccount), deref(j.DestAccount)))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdHistory(cfg config) string {
	jobs, err := b.DB.RecentJobs(cfg.userID, 10)
	if err != nil {
		return "Could not read the history: " + err.Error()
	}
	if len(jobs) == 0 {
		return "No jobs yet."
	}
	lines := []string{"Last 10 jobs:"}
	for _, j := range jobs {
		when := "-"
		if j.StartedAt != nil {
			when = j.StartedAt.Format("01-02 15:04")
		}
		lines = append(lines, fmt.Sprintf("  #%d [%s] %s  %s -> %s",
			j.ID, j.Status, when, deref(j.SourceAccount), deref(j.DestAccount)))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdStatus(cfg config, jobID int64) string {
	job, err := b.DB.JobByIDForUser(jobID, cfg.userID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Sprintf("Job #%d not found.", jobID)
	}
	if err != nil {
		return "Could not read that job: " + err.Error()
	}

	lines := []string{
		fmt.Sprintf("Job #%d: %s", job.ID, job.Status),
		fmt.Sprintf("%s -> %s", deref(job.SourceAccount), deref(job.DestAccount)),
		fmt.Sprintf("Messages: %d   Bytes: %d   Errors: %d",
			job.MessagesTransferred, job.BytesTransferred, job.ErrorsCount),
	}
	if job.StartedAt != nil {
		lines = append(lines, "Started: "+job.StartedAt.Format("2006-01-02 15:04:05"))
	}
	if job.FinishedAt != nil {
		lines = append(lines, "Finished: "+job.FinishedAt.Format("2006-01-02 15:04:05"))
	}

	if live := b.Runner.Get(jobID); live != nil {
		p := live.Progress()
		if p.Percent > 0 {
			lines = append(lines, fmt.Sprintf("Progress: %.1f%%  folder %s  ETA %s", p.Percent, p.Folder, p.ETA))
		}
	} else if job.Summary != nil && *job.Summary != "" {
		lines = append(lines, "", strings.TrimSpace(*job.Summary))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdSync(cfg config, profileID int64) string {
	jobID, err := b.Starter.StartProfile(cfg.userID, profileID, nil, false)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Sprintf("Profile %d not found. Use /profiles to list them.", profileID)
	}
	if err != nil {
		return "Could not start the sync: " + err.Error()
	}
	return fmt.Sprintf("Started job #%d. I will message you as it progresses.\nCheck with: /status %d", jobID, jobID)
}

func (b *Bot) cmdStop(cfg config, jobID int64) string {
	if _, err := b.DB.JobByIDForUser(jobID, cfg.userID); err != nil {
		return fmt.Sprintf("Job #%d not found.", jobID)
	}
	if !b.Runner.Abort(jobID) {
		return fmt.Sprintf("Job #%d is not running.", jobID)
	}
	if err := b.DB.MarkAborted(jobID); err != nil {
		return "Aborted, but could not record it: " + err.Error()
	}
	return fmt.Sprintf("Job #%d aborted.", jobID)
}

// cmdRun syncs one mailbox from credentials typed straight into the chat.
func (b *Bot) cmdRun(ctx context.Context, cfg config, body string, msg *message) string {
	if strings.TrimSpace(body) == "" {
		return runUsage
	}

	// Delete before parsing, not after: a message that fails to parse can still
	// carry a working password, and leaving the typo'd ones in the chat is the
	// opposite of the point.
	b.deleteMessage(ctx, cfg.token, msg.Chat.ID, msg.MessageID)

	rows, err := parseRun(body)
	if err != nil {
		return err.Error()
	}

	// Several lines are a list of mailboxes, not a one-off: make a batch and
	// make the user confirm it.
	if len(rows) > 1 {
		batch, err := b.Batch.CreateFromRows(cfg.userID, rows, "telegram-run", defaultBatchOptions())
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("That was %d mailboxes, so I made a batch instead.\n\n%s\n\n%s\nRun it with: /batchgo %d",
			len(rows), describeBatch(batch, rows), credentialWarning, batch.ID)
	}

	row := rows[0]
	extra := map[string]any{}
	for k, v := range row.ExtraOptions {
		extra[k] = v
	}
	// ssl1/ssl2 are managed by the runner and blocked from extra_options, so
	// accept them here as their own thing.
	ssl1 := !popFlag(extra, "nossl1")
	ssl2 := !popFlag(extra, "nossl2")
	dryRun := popFlag(extra, "dry")

	if _, rejected := runner.SanitizeOptions(extra); len(rejected) > 0 {
		return "Unsupported or unsafe imapsync options: " + strings.Join(rejected, ", ")
	}

	jobID, err := b.Starter.StartAdhoc(cfg.userID, api.AdhocParams{
		Host1: row.Host1, User1: row.User1, Password1: row.Password1,
		Host2: row.Host2, User2: row.User2, Password2: row.Password2,
		SSL1: ssl1, SSL2: ssl2, ExtraOptions: extra, DryRun: dryRun,
	})
	if err != nil {
		return "Could not start the sync: " + err.Error()
	}
	return fmt.Sprintf("Started job #%d\n%s\n\n%s\nFollow it with: /status %d",
		jobID, describeRow(row, dryRun), credentialWarning, jobID)
}

func (b *Bot) cmdBatchUpload(ctx context.Context, cfg config, msg *message) string {
	name := msg.Document.FileName
	if name == "" {
		name = "telegram.csv"
	}

	content, err := b.downloadFile(ctx, cfg.token, msg.Document.FileID)
	if err != nil {
		if err.Error() == "too large" {
			return fmt.Sprintf("That file is larger than %d kB. Split it or use the web UI.", maxCSVBytes/1000)
		}
		return "Could not download the file: " + redact(err, cfg.token)
	}

	// The uploaded file holds mailbox passwords; take it out of the chat as
	// soon as it is read. Telegram still keeps it on their servers.
	b.deleteMessage(ctx, cfg.token, msg.Chat.ID, msg.MessageID)

	rows, err := services.ParseCSV(string(content))
	if err != nil {
		return err.Error()
	}
	batch, err := b.Batch.CreateFromRows(cfg.userID, rows, name, defaultBatchOptions())
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%s\n\n%s\nRun it with: /batchgo %d",
		describeBatch(batch, rows), credentialWarning, batch.ID)
}

func (b *Bot) cmdBatchGo(cfg config, batchID int64) string {
	batch, err := b.DB.BatchByID(batchID, cfg.userID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Sprintf("Batch #%d not found.", batchID)
	}
	if err != nil {
		return "Could not read that batch: " + err.Error()
	}
	if batch.Status == "running" {
		return fmt.Sprintf("Batch #%d is already running.", batchID)
	}
	if batch.BatchData == nil {
		return fmt.Sprintf("Batch #%d has no data left (already finished).", batchID)
	}

	if err := b.DB.SetBatchStatus(batch.ID, "running"); err != nil {
		return "Could not start it: " + err.Error()
	}
	b.Batch.Start(batch.ID, cfg.userID, batch.BatchData)
	return fmt.Sprintf("Batch #%d started: %d mailbox(es), one at a time.", batch.ID, batch.TotalRows)
}

// ----------------------------------------------------------- notifications

// NotifyJobFinished is wired into the job completion callback.
func (b *Bot) NotifyJobFinished(jobID int64, status, summary string, userID int64) {
	b.milestoneMu.Lock()
	delete(b.milestones, jobID)
	b.milestoneMu.Unlock()

	cfg := b.current()
	if !cfg.usable() || userID != cfg.userID {
		return
	}
	if status == "completed" && !cfg.notifyOnSuccess {
		return
	}
	if status == "failed" && !cfg.notifyOnFailure {
		return
	}

	route, messages := "", 0
	if job, err := b.DB.JobByID(jobID); err == nil {
		route = deref(job.SourceAccount) + " -> " + deref(job.DestAccount)
		messages = job.MessagesTransferred
	}

	label := strings.ToUpper(status)
	switch status {
	case "completed":
		label = "OK"
	case "failed":
		label = "FAILED"
	case "aborted":
		label = "ABORTED"
	}

	text := fmt.Sprintf("[%s] Job #%d\n%s\nMessages transferred: %d", label, jobID, route, messages)
	if summary != "" {
		text += "\n\n" + strings.TrimSpace(summary)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, chatID := range cfg.chatIDs {
		b.sendMessage(ctx, cfg.token, chatID, text)
	}
}

// OnProgress is registered with the runner, which reports once a second while
// a job runs. Only crossing a step is announced: nobody wants a message a
// second.
func (b *Bot) OnProgress(jobID int64, p runner.Progress) {
	cfg := b.current()
	if !cfg.usable() || !cfg.notifyOnProgress {
		return
	}
	reached := int(p.Percent) / cfg.progressStep * cfg.progressStep
	if reached <= 0 || reached >= 100 {
		return
	}

	b.milestoneMu.Lock()
	if b.milestones[jobID] >= reached {
		b.milestoneMu.Unlock()
		return
	}
	b.milestones[jobID] = reached
	b.milestoneMu.Unlock()

	job, err := b.DB.JobByID(jobID)
	if err != nil || job.UserID != cfg.userID {
		return
	}

	text := fmt.Sprintf("[%d%%] Job #%d\n%s -> %s\n%s\n%d message(s), %s copied so far",
		reached, jobID, deref(job.SourceAccount), deref(job.DestAccount),
		p.Folder, p.Messages, humanBytes(p.Bytes))
	if p.ETA != "" {
		text += "\nAbout " + p.ETA + " left"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, chatID := range cfg.chatIDs {
		b.sendMessage(ctx, cfg.token, chatID, text)
	}
}

// ------------------------------------------------------------------ helpers

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

func defaultBatchOptions() map[string]any {
	return map[string]any{"dry_run": false, "ssl1": true, "ssl2": true, "automap": true, "usecache": true}
}

// describeBatch previews a batch. It never includes passwords.
func describeBatch(batch *store.BatchRun, rows []services.Row) string {
	lines := []string{fmt.Sprintf("Batch #%d from %s: %d row(s)", batch.ID, batch.Filename, len(rows))}
	for i, r := range rows {
		if i == 10 {
			lines = append(lines, fmt.Sprintf("  ... and %d more", len(rows)-10))
			break
		}
		lines = append(lines, fmt.Sprintf("  %d. %s on %s -> %s on %s",
			i+1, r.User1, r.Host1, r.User2, r.Host2))
	}
	lines = append(lines, "Options: automap, usecache")
	return strings.Join(lines, "\n")
}

func popFlag(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	delete(m, key)
	b, _ := v.(bool)
	return b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
