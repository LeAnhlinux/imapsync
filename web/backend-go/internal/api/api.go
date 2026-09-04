// Package api serves the HTTP surface the React frontend talks to.
//
// The routes and JSON shapes are the ones the Python backend exposed: the
// frontend is unchanged, so it doubles as the contract test.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/imapsync/web-backend/internal/config"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

// Bot is the slice of the Telegram bot the API needs, kept as an interface so
// the api package does not import telegram (which imports services, which
// imports api's siblings).
type Bot interface {
	Restart()
	Status() map[string]any
	SendTest(token string, chatIDs []int64) (bool, string)
}

type Server struct {
	Cfg    *config.Config
	DB     *store.DB
	Sec    *security.Manager
	Runner *runner.Runner
	Bot    Bot

	// Starter launches syncs; an interface to avoid an import cycle with
	// services, which needs the store and runner this Server already holds.
	Starter   Starter
	Batch     BatchService
	Folders   FolderBrowser
	Mailer    Mailer
	Cron      CronService
	Hooks     WebhookService
	Retention RetentionService
}

// RetentionService applies the saved history-cleanup policy.
type RetentionService interface {
	SweepUser(userID int64, keepDays int) (int, error)
}

// WebhookService delivers job events to registered URLs.
type WebhookService interface {
	Dispatch(event string, userID, jobID int64)
	Test(hookID int64) (int, error)
}

type Starter interface {
	StartProfile(userID, profileID int64, folders []string, dryRun bool, override map[string]any) (int64, error)
	StartAdhoc(userID int64, p AdhocParams) (int64, error)
}

// AdhocParams carries credentials given directly instead of via a profile.
type AdhocParams struct {
	Host1, User1, Password1 string
	Host2, User2, Password2 string
	SSL1, SSL2              bool
	ExtraOptions            map[string]any
	Folders                 []string
	DryRun                  bool
}

type BatchService interface {
	Create(userID int64, csvText, filename string, opts map[string]any) (*store.BatchRun, error)
	Start(batchID, userID int64, data store.JSONMap)
	// Cancel reports whether the batch was running.
	Cancel(batchID int64) bool
}

type FolderBrowser interface {
	Browse(host, user, password string, ssl bool) ([]FolderInfo, error)
}

type FolderInfo struct {
	Name     string `json:"name"`
	Messages int    `json:"messages"`
	Size     int64  `json:"size"`
}

type Mailer interface {
	SendTest(n *store.NotificationSettings, password string) error
}

type CronService interface {
	Reload()
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Post("/auth/register", s.register)
		r.Post("/auth/login", s.login)

		// The WebSocket carries its token in the query string, so it
		// authenticates itself rather than going through the middleware.
		r.Get("/ws/jobs/{id}", s.jobWebSocket)

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)

			// Scopes apply only to API keys; a browser session already
			// authenticated as the user and is not scope-limited.
			r.Get("/auth/me", s.me)

			r.With(requireScope("profiles:read")).Get("/profiles", s.listProfiles)
			r.With(requireScope("profiles:read")).Get("/profiles/{id}", s.getProfile)
			r.With(requireScope("profiles:write")).Post("/profiles", s.createProfile)
			r.With(requireScope("profiles:write")).Put("/profiles/{id}", s.updateProfile)
			r.With(requireScope("profiles:write")).Delete("/profiles/{id}", s.deleteProfile)

			r.With(requireScope("sync:write")).Post("/sync/start", s.startSync)
			r.With(requireScope("sync:write")).Post("/sync/{id}/abort", s.abortSync)
			r.With(requireScope("sync:write")).Delete("/sync/{id}", s.deleteSync)
			r.With(requireScope("sync:write")).Post("/history/purge", s.purgeHistory)
			r.With(requireScope("sync:read")).Get("/history/retention", s.getRetention)
			r.With(requireScope("sync:write")).Put("/history/retention", s.updateRetention)
			r.With(requireScope("sync:write")).Post("/history/retention/run", s.runRetention)
			r.With(requireScope("sync:read")).Get("/sync/running", s.runningSyncs)
			r.With(requireScope("sync:read")).Get("/sync/{id}", s.getSync)
			r.With(requireScope("sync:read")).Get("/sync/{id}/log", s.syncLog)

			r.With(requireScope("sync:read")).Get("/history", s.history)
			r.With(requireScope("sync:read")).Get("/history/stats", s.historyStats)

			r.With(requireScope("schedules:read")).Get("/schedules", s.listSchedules)
			r.With(requireScope("schedules:write")).Post("/schedules", s.createSchedule)
			r.With(requireScope("schedules:write")).Put("/schedules/{id}", s.updateSchedule)
			r.With(requireScope("schedules:write")).Delete("/schedules/{id}", s.deleteSchedule)

			r.With(requireScope("profiles:read")).Post("/folders/browse", s.browseFolders)

			r.With(requireScope("batch:write")).Post("/batch/upload", s.batchUpload)
			r.With(requireScope("batch:write")).Post("/batch/upload-text", s.batchUploadText)
			r.With(requireScope("batch:write")).Post("/batch/{id}/start", s.batchStart)
			r.With(requireScope("batch:write")).Post("/batch/{id}/abort", s.batchAbort)
			r.With(requireScope("sync:read")).Get("/batch/{id}", s.getBatch)

			// Settings, keys and webhooks are administration: an API key needs
			// the admin scope, and issuing keys from a key is deliberate.
			r.Group(func(r chi.Router) {
				r.Use(requireScope("admin"))

				r.Get("/notifications/settings", s.getNotifications)
				r.Put("/notifications/settings", s.updateNotifications)
				r.Post("/notifications/test", s.testNotifications)

				r.Get("/api-keys", s.listAPIKeys)
				r.Post("/api-keys", s.createAPIKey)
				r.Get("/api-keys/scopes", s.listScopes)
				r.Post("/api-keys/{id}/revoke", s.revokeAPIKey)
				r.Delete("/api-keys/{id}", s.deleteAPIKey)

				r.Get("/webhooks", s.listWebhooks)
				r.Post("/webhooks", s.createWebhook)
				r.Get("/webhooks/events", s.listEvents)
				r.Put("/webhooks/{id}", s.updateWebhook)
				r.Delete("/webhooks/{id}", s.deleteWebhook)
				r.Post("/webhooks/{id}/test", s.testWebhook)

				r.Get("/telegram/settings", s.getTelegram)
				r.Put("/telegram/settings", s.updateTelegram)
				r.Get("/telegram/status", s.telegramStatus)
				r.Post("/telegram/test", s.testTelegram)
			})
		})
	})

	// Serve the built frontend when it is there, with SPA fallback.
	if static := s.Cfg.StaticDir(); static != "" {
		r.NotFound(spaHandler(static))
	}
	return r
}

// ------------------------------------------------------------------ helpers

type ctxKey string

const userKey ctxKey = "user"

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError matches FastAPI's error body so the frontend's
// err.response.data.detail keeps working.
func writeError(w http.ResponseWriter, code int, detail string) {
	writeJSON(w, code, map[string]string{"detail": detail})
}

// maxJSONBody caps a request body. nginx allows 50 MB so that batch CSV
// uploads fit, but no JSON endpoint has any use for that, and an unbounded
// decode let an unauthenticated caller hand the login throttle a 40 MB key.
const maxJSONBody = 1 << 20 // 1 MiB

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return false
	}
	return true
}

func urlID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// storeErr maps a store error onto a response, returning true if it handled one.
func storeErr(w http.ResponseWriter, err error, notFound string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, notFound)
		return true
	}
	writeError(w, http.StatusInternalServerError, err.Error())
	return true
}

func queryBool(r *http.Request, name string, fallback bool) bool {
	v := r.URL.Query().Get(name)
	switch v {
	case "true", "True", "1", "yes":
		return true
	case "false", "False", "0", "no":
		return false
	}
	return fallback
}

func queryInt(r *http.Request, name string, fallback int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return n
	}
	return fallback
}
