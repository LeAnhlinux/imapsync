package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/imapsync/web-backend/internal/store"
)

func (s *Server) startSync(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ProfileID *int64 `json:"profile_id"`

		Host1        string         `json:"host1"`
		User1        string         `json:"user1"`
		Password1    string         `json:"password1"`
		SSL1         *bool          `json:"ssl1"`
		Host2        string         `json:"host2"`
		User2        string         `json:"user2"`
		Password2    string         `json:"password2"`
		SSL2         *bool          `json:"ssl2"`
		ExtraOptions map[string]any `json:"extra_options"`
		Folders      []string       `json:"folders"`
		DryRun       bool           `json:"dry_run"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	user := userFrom(r)

	var jobID int64
	var err error

	if b.ProfileID != nil && *b.ProfileID > 0 {
		// extra_options used to be dropped on this branch — accepted by the API,
		// echoed back on the review screen, and never applied. Validate it the
		// same way the ad-hoc branch does, then let it override the profile.
		extra, ok := validateOptions(w, b.ExtraOptions)
		if !ok {
			return
		}
		jobID, err = s.Starter.StartProfile(user.ID, *b.ProfileID, b.Folders, b.DryRun, extra)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Profile not found")
			return
		}
	} else {
		if b.Host1 == "" || b.User1 == "" || b.Password1 == "" ||
			b.Host2 == "" || b.User2 == "" || b.Password2 == "" {
			writeError(w, http.StatusBadRequest, "Provide profile_id or all connection params")
			return
		}
		extra, ok := validateOptions(w, b.ExtraOptions)
		if !ok {
			return
		}
		jobID, err = s.Starter.StartAdhoc(user.ID, AdhocParams{
			Host1: b.Host1, User1: b.User1, Password1: b.Password1,
			Host2: b.Host2, User2: b.User2, Password2: b.Password2,
			SSL1: boolOr(b.SSL1, true), SSL2: boolOr(b.SSL2, true),
			ExtraOptions: extra, Folders: b.Folders, DryRun: b.DryRun,
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	job, err := s.DB.JobByIDForUser(jobID, user.ID)
	if storeErr(w, err, "Job not found") {
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) runningSyncs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.DB.RunningJobs(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) getSync(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}
	job, err := s.DB.JobByIDForUser(id, userFrom(r).ID)
	if storeErr(w, err, "Job not found") {
		return
	}
	if wait := min(queryInt(r, "wait", 0), maxJobWait); wait > 0 && job.Status == "running" {
		job, err = s.waitForJob(r.Context(), job, time.Duration(wait)*time.Second)
		if storeErr(w, err, "Job not found") {
			return
		}
	}
	writeJSON(w, http.StatusOK, job)
}

// maxJobWait caps ?wait=, in seconds. Long enough that a waiting program makes
// one request a minute or two, short enough to stay under client timeouts.
const maxJobWait = 120

// waitForJob holds GET /sync/{id}?wait=N until the job stops running or the
// time is up, whichever is first. A program waiting for a sync otherwise has to
// poll, and every poll spends its key's rate limit; this is one request.
func (s *Server) waitForJob(ctx context.Context, job *store.SyncJob, d time.Duration) (*store.SyncJob, error) {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	// The runner closes done before OnComplete writes the outcome, so done only
	// means "look now" — the row decides, and the ticker covers the gap.
	var done <-chan struct{}
	if live := s.Runner.Get(job.ID); live != nil {
		done = live.Done()
	}
	for {
		select {
		case <-ctx.Done():
			return job, nil
		case <-deadline.C:
			return job, nil
		case <-done:
			done = nil
		case <-tick.C:
		}
		next, err := s.DB.JobByIDForUser(job.ID, job.UserID)
		if err != nil {
			return nil, err
		}
		if job = next; job.Status != "running" {
			return job, nil
		}
	}
}

func (s *Server) abortSync(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}
	if _, err := s.DB.JobByIDForUser(id, userFrom(r).ID); storeErr(w, err, "Job not found") {
		return
	}
	if !s.Runner.Abort(id) {
		writeError(w, http.StatusBadRequest, "Job not running or already finished")
		return
	}
	// Set the status before the process exits, so the completion callback sees
	// "aborted" and keeps it rather than recording a failure.
	if err := s.DB.MarkAborted(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "aborted"})
}

func (s *Server) syncLog(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}
	if _, err := s.DB.JobByIDForUser(id, userFrom(r).ID); storeErr(w, err, "Job not found") {
		return
	}
	text := s.Runner.LogFor(id)
	part, next := text, len(text)
	if q := r.URL.Query(); q.Has("tail") || q.Has("offset") {
		part, next = sliceLog(text, queryInt(r, "offset", 0), min(queryInt(r, "tail", 0), maxLogTail))
	}
	writeJSON(w, http.StatusOK, map[string]any{"log": part, "size": len(text), "next_offset": next})
}

const (
	// maxLogPage bounds one ?offset= read. A finished migration's log runs to
	// megabytes, more than a program — an MCP tool feeding a model above all —
	// can take in one piece.
	maxLogPage = 256 << 10
	maxLogTail = 5000
)

// sliceLog picks the part of a log a request asked for, and the offset to ask
// for next. Without either parameter the handler skips it and returns the whole
// log, which is what the web UI reads. tail wins over offset: it is the "what is
// happening now" read, and its next offset lets the caller follow on from there.
func sliceLog(text string, offset, tail int) (string, int) {
	if tail > 0 {
		start := len(text)
		// A trailing newline ends the last line rather than starting an empty one.
		if start > 0 && text[start-1] == '\n' {
			start--
		}
		for n := 0; n < tail && start > 0; n++ {
			start = strings.LastIndexByte(text[:start], '\n') + 1
			// Step back over the newline to reach the line before, except after
			// the last line wanted.
			if n < tail-1 && start > 0 {
				start--
			}
		}
		return text[start:], len(text)
	}
	offset = min(max(offset, 0), len(text))
	end := min(offset+maxLogPage, len(text))
	// End a page on a line boundary, so no line — or UTF-8 sequence — is split
	// across two reads.
	if end < len(text) {
		if i := strings.LastIndexByte(text[offset:end], '\n'); i >= 0 {
			end = offset + i + 1
		}
	}
	return text[offset:end], end
}

// jobWebSocket streams a job's output. It authenticates from the query string
// because browsers cannot set headers on a WebSocket handshake.
func (s *Server) jobWebSocket(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chiURLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad job id", http.StatusBadRequest)
		return
	}

	userID, err := s.Sec.UserIDFromToken(r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	// Job logs contain message subjects and headers — only the owner may read.
	if !s.DB.JobBelongsTo(jobID, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	ctx := r.Context()
	defer conn.CloseNow()

	job := s.Runner.Get(jobID)
	if job == nil {
		// Already finished: replay the saved log, then say so and close.
		for _, line := range strings.Split(s.Runner.LogFor(jobID), "\n") {
			if wsjson.Write(ctx, conn, map[string]any{"type": "log", "data": line}) != nil {
				return
			}
		}
		status := "completed"
		if j, err := s.DB.JobByID(jobID); err == nil {
			status = j.Status
		}
		_ = wsjson.Write(ctx, conn, map[string]any{"type": "status", "status": status})
		_ = conn.Close(websocket.StatusNormalClosure, "")
		return
	}

	events, cancel := job.Subscribe()
	defer cancel()

	for _, e := range job.Replay() {
		if wsjson.Write(ctx, conn, e) != nil {
			return
		}
	}

	// Drain the client so a browser close is noticed promptly.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				cancel()
				return
			}
		}
	}()

	for e := range events {
		if wsjson.Write(ctx, conn, e) != nil {
			return
		}
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func joinSorted(items []string) string {
	sort.Strings(items)
	return strings.Join(items, ", ")
}

func itoa(n int) string { return strconv.Itoa(n) }

func (s *Server) deleteSync(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}
	_, err := s.DB.DeleteJob(id, userFrom(r).ID)
	if errors.Is(err, store.ErrJobRunning) {
		writeError(w, http.StatusConflict, "This job is still running. Stop it first, then delete it.")
		return
	}
	if storeErr(w, err, "Job not found") {
		return
	}
	s.Runner.DeleteLog(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// purgeHistory clears finished jobs older than a cutoff, with their logs.
func (s *Server) purgeHistory(w http.ResponseWriter, r *http.Request) {
	var b struct {
		OlderThanDays int `json:"older_than_days"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.OlderThanDays < 0 {
		writeError(w, http.StatusBadRequest, "older_than_days cannot be negative")
		return
	}

	cutoff := store.NaiveTime{Time: store.NowVN().AddDate(0, 0, -b.OlderThanDays)}
	ids, err := s.DB.PurgeJobs(userFrom(r).ID, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, id := range ids {
		s.Runner.DeleteLog(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(ids)})
}

func (s *Server) getRetention(w http.ResponseWriter, r *http.Request) {
	rt, err := s.DB.RetentionFor(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (s *Server) updateRetention(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled  *bool `json:"enabled"`
		KeepDays *int  `json:"keep_days"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	rt, err := s.DB.RetentionFor(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if b.Enabled != nil {
		rt.Enabled = *b.Enabled
	}
	if b.KeepDays != nil {
		// A floor of one day: "keep zero days" would delete a job the moment
		// it finished, which is never what someone means by retention.
		if *b.KeepDays < 1 || *b.KeepDays > 3650 {
			writeError(w, http.StatusBadRequest, "keep_days must be between 1 and 3650")
			return
		}
		rt.KeepDays = *b.KeepDays
	}
	if err := s.DB.SaveRetention(rt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rt, _ = s.DB.RetentionFor(userFrom(r).ID)
	writeJSON(w, http.StatusOK, rt)
}

// runRetention applies the saved policy immediately, so the setting can be
// verified without waiting for the nightly sweep.
func (s *Server) runRetention(w http.ResponseWriter, r *http.Request) {
	rt, err := s.DB.RetentionFor(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !rt.Enabled {
		writeError(w, http.StatusBadRequest, "Automatic cleanup is switched off.")
		return
	}
	n, err := s.Retention.SweepUser(userFrom(r).ID, rt.KeepDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}
