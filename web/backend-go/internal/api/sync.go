package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

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
		jobID, err = s.Starter.StartProfile(user.ID, *b.ProfileID, b.Folders, b.DryRun)
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
	writeJSON(w, http.StatusOK, job)
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
	writeJSON(w, http.StatusOK, map[string]string{"log": s.Runner.LogFor(id)})
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
