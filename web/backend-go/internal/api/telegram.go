package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/imapsync/web-backend/internal/store"
)

// ParseChatIDs turns the comma-separated field from the settings form into ids.
func ParseChatIDs(raw string) []int64 {
	var out []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

type telegramOut struct {
	Enabled          bool   `json:"enabled"`
	HasToken         bool   `json:"has_token"`
	AllowedChatIDs   string `json:"allowed_chat_ids"`
	UserID           *int64 `json:"user_id"`
	NotifyOnSuccess  bool   `json:"notify_on_success"`
	NotifyOnFailure  bool   `json:"notify_on_failure"`
	NotifyOnProgress bool   `json:"notify_on_progress"`
	ProgressStep     int    `json:"progress_step"`
}

// toOut leaves the token out: it is write-only and never leaves the server.
func toOut(t *store.TelegramSettings) telegramOut {
	return telegramOut{
		Enabled:          t.Enabled,
		HasToken:         t.BotTokenEnc != nil && *t.BotTokenEnc != "",
		AllowedChatIDs:   t.AllowedChatIDs,
		UserID:           t.UserID,
		NotifyOnSuccess:  t.NotifyOnSuccess,
		NotifyOnFailure:  t.NotifyOnFailure,
		NotifyOnProgress: t.NotifyOnProgress,
		ProgressStep:     t.ProgressStep,
	}
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !userFrom(r).IsAdmin {
		writeError(w, http.StatusForbidden, "Admin only")
		return false
	}
	return true
}

func (s *Server) getTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	t, err := s.DB.TelegramSettingsOrCreate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toOut(t))
}

func (s *Server) updateTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var b struct {
		Enabled          *bool   `json:"enabled"`
		BotToken         *string `json:"bot_token"`
		AllowedChatIDs   *string `json:"allowed_chat_ids"`
		UserID           *int64  `json:"user_id"`
		NotifyOnSuccess  *bool   `json:"notify_on_success"`
		NotifyOnFailure  *bool   `json:"notify_on_failure"`
		NotifyOnProgress *bool   `json:"notify_on_progress"`
		ProgressStep     *int    `json:"progress_step"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}

	t, err := s.DB.TelegramSettingsOrCreate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if b.BotToken != nil {
		// An empty string means "forget the token", not "store an empty one".
		if *b.BotToken == "" {
			t.BotTokenEnc = nil
		} else {
			enc, err := s.Sec.Encrypt(*b.BotToken)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			t.BotTokenEnc = &enc
		}
	}
	if b.AllowedChatIDs != nil {
		t.AllowedChatIDs = *b.AllowedChatIDs
	}
	if b.UserID != nil {
		if _, err := s.DB.UserByID(*b.UserID); err != nil {
			writeError(w, http.StatusBadRequest, "No such user")
			return
		}
		t.UserID = b.UserID
	}
	if b.Enabled != nil {
		t.Enabled = *b.Enabled
	}
	if b.NotifyOnSuccess != nil {
		t.NotifyOnSuccess = *b.NotifyOnSuccess
	}
	if b.NotifyOnFailure != nil {
		t.NotifyOnFailure = *b.NotifyOnFailure
	}
	if b.NotifyOnProgress != nil {
		t.NotifyOnProgress = *b.NotifyOnProgress
	}
	if b.ProgressStep != nil {
		if *b.ProgressStep < 1 || *b.ProgressStep > 50 {
			writeError(w, http.StatusBadRequest, "progress_step must be between 1 and 50")
			return
		}
		t.ProgressStep = *b.ProgressStep
	}

	if t.Enabled && (t.BotTokenEnc == nil || t.AllowedChatIDs == "" || t.UserID == nil) {
		writeError(w, http.StatusBadRequest,
			"A bot token, at least one chat id and an app user are all required to enable the bot")
		return
	}

	if err := s.DB.SaveTelegramSettings(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Pick up the new settings without a service restart.
	if s.Bot != nil {
		s.Bot.Restart()
	}
	writeJSON(w, http.StatusOK, toOut(t))
}

func (s *Server) telegramStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.Bot == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"running": false, "configured": false, "bot_username": nil, "error": nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, s.Bot.Status())
}

func (s *Server) testTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	t, err := s.DB.TelegramSettings()
	if err != nil || t.BotTokenEnc == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": "No bot token saved yet"})
		return
	}
	chatIDs := ParseChatIDs(t.AllowedChatIDs)
	if len(chatIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": "No chat ids configured"})
		return
	}
	if s.Bot == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": "Bot is not available"})
		return
	}
	token, err := s.Sec.Decrypt(*t.BotTokenEnc)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": err.Error()})
		return
	}
	ok, detail := s.Bot.SendTest(token, chatIDs)
	status := "ok"
	if !ok {
		status = "error"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "detail": detail})
}
