package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/imapsync/web-backend/internal/store"
)

// Events a webhook can subscribe to. "*" in the stored list means all of them.
var ValidEvents = []string{"job.started", "job.completed", "job.failed", "job.aborted"}

func validEvent(e string) bool {
	if e == "*" {
		return true
	}
	for _, v := range ValidEvents {
		if v == e {
			return true
		}
	}
	return false
}

type webhookCreated struct {
	*store.Webhook
	// Secret is returned once, at creation: it is what the receiver needs to
	// verify the HMAC signature, and it is encrypted at rest afterwards.
	Secret string `json:"secret"`
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.DB.WebhooksByUser(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hooks)
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var b struct {
		URL      string   `json:"url"`
		Events   []string `json:"events"`
		IsActive *bool    `json:"is_active"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}

	parsed, err := url.Parse(strings.TrimSpace(b.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "url must be an absolute http:// or https:// address")
		return
	}
	if len(b.Events) == 0 {
		b.Events = []string{"*"}
	}
	for _, e := range b.Events {
		if !validEvent(e) {
			writeError(w, http.StatusBadRequest,
				"Unknown event "+e+". Valid: "+strings.Join(ValidEvents, ", ")+", or *")
			return
		}
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	secretEnc, err := s.Sec.Encrypt(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hook := &store.Webhook{
		UserID: userFrom(r).ID, URL: parsed.String(),
		Events: strings.Join(b.Events, ","), IsActive: boolOr(b.IsActive, true),
	}
	if err := s.DB.CreateWebhook(hook, secretEnc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, webhookCreated{Webhook: hook, Secret: secret})
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Webhook not found")
		return
	}
	hook, err := s.DB.WebhookByID(id, userFrom(r).ID)
	if storeErr(w, err, "Webhook not found") {
		return
	}
	var b struct {
		URL      *string  `json:"url"`
		Events   []string `json:"events"`
		IsActive *bool    `json:"is_active"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.URL != nil {
		parsed, err := url.Parse(strings.TrimSpace(*b.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			writeError(w, http.StatusBadRequest, "url must be an absolute http:// or https:// address")
			return
		}
		hook.URL = parsed.String()
	}
	if len(b.Events) > 0 {
		for _, e := range b.Events {
			if !validEvent(e) {
				writeError(w, http.StatusBadRequest, "Unknown event "+e)
				return
			}
		}
		hook.Events = strings.Join(b.Events, ",")
	}
	if b.IsActive != nil {
		hook.IsActive = *b.IsActive
	}
	if err := s.DB.UpdateWebhook(hook); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hook)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Webhook not found")
		return
	}
	if err := s.DB.DeleteWebhook(id, userFrom(r).ID); storeErr(w, err, "Webhook not found") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Webhook not found")
		return
	}
	if _, err := s.DB.WebhookByID(id, userFrom(r).ID); storeErr(w, err, "Webhook not found") {
		return
	}
	status, err := s.Hooks.Test(id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "http_status": status})
}

func (s *Server) listEvents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ValidEvents)
}
