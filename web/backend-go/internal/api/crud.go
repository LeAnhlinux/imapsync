package api

import (
	"errors"
	"net/http"

	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/store"
)

// ---------------------------------------------------------------- profiles

type profileBody struct {
	Name         *string        `json:"name"`
	Host1        *string        `json:"host1"`
	User1        *string        `json:"user1"`
	Password1    *string        `json:"password1"`
	SSL1         *bool          `json:"ssl1"`
	Host2        *string        `json:"host2"`
	User2        *string        `json:"user2"`
	Password2    *string        `json:"password2"`
	SSL2         *bool          `json:"ssl2"`
	ExtraOptions map[string]any `json:"extra_options"`
}

// validateOptions rejects unsupported imapsync options at the API boundary so
// the user sees why, rather than having them dropped silently in the runner.
func validateOptions(w http.ResponseWriter, extra map[string]any) (store.JSONMap, bool) {
	accepted, rejected := runner.SanitizeOptions(extra)
	if len(rejected) > 0 {
		writeError(w, http.StatusBadRequest,
			"Unsupported or unsafe imapsync options: "+joinSorted(rejected))
		return nil, false
	}
	out := store.JSONMap{}
	for k, v := range accepted {
		out[k] = v
	}
	return out, true
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	ps, err := s.DB.ProfilesByUser(userFrom(r).ID)
	if storeErr(w, err, "Profile not found") {
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var b profileBody
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.Name == nil || b.Host1 == nil || b.User1 == nil || b.Password1 == nil ||
		b.Host2 == nil || b.User2 == nil || b.Password2 == nil {
		writeError(w, http.StatusBadRequest, "name, host1, user1, password1, host2, user2 and password2 are required")
		return
	}
	extra, ok := validateOptions(w, b.ExtraOptions)
	if !ok {
		return
	}

	enc1, err := s.Sec.Encrypt(*b.Password1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enc2, err := s.Sec.Encrypt(*b.Password2)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	p := &store.Profile{
		UserID: userFrom(r).ID, Name: *b.Name,
		Host1: *b.Host1, User1: *b.User1, Password1Enc: enc1, SSL1: boolOr(b.SSL1, true),
		Host2: *b.Host2, User2: *b.User2, Password2Enc: enc2, SSL2: boolOr(b.SSL2, true),
		ExtraOptions: extra,
	}
	if err := s.DB.CreateProfile(p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, _ = s.DB.ProfileByID(p.ID, p.UserID)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	p, err := s.DB.ProfileByID(id, userFrom(r).ID)
	if storeErr(w, err, "Profile not found") {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	p, err := s.DB.ProfileByID(id, userFrom(r).ID)
	if storeErr(w, err, "Profile not found") {
		return
	}
	var b profileBody
	if !decodeJSON(w, r, &b) {
		return
	}

	if b.Name != nil {
		p.Name = *b.Name
	}
	if b.Host1 != nil {
		p.Host1 = *b.Host1
	}
	if b.User1 != nil {
		p.User1 = *b.User1
	}
	if b.SSL1 != nil {
		p.SSL1 = *b.SSL1
	}
	if b.Host2 != nil {
		p.Host2 = *b.Host2
	}
	if b.User2 != nil {
		p.User2 = *b.User2
	}
	if b.SSL2 != nil {
		p.SSL2 = *b.SSL2
	}
	// An omitted password keeps the stored one; the API never hands it back.
	if b.Password1 != nil {
		if p.Password1Enc, err = s.Sec.Encrypt(*b.Password1); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if b.Password2 != nil {
		if p.Password2Enc, err = s.Sec.Encrypt(*b.Password2); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if b.ExtraOptions != nil {
		extra, ok := validateOptions(w, b.ExtraOptions)
		if !ok {
			return
		}
		p.ExtraOptions = extra
	}

	if err := s.DB.UpdateProfile(p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, _ = s.DB.ProfileByID(p.ID, p.UserID)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	if err := s.DB.DeleteProfile(id, userFrom(r).ID); storeErr(w, err, "Profile not found") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------- history

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	filter := store.HistoryFilter{
		UserID:  userFrom(r).ID,
		Status:  r.URL.Query().Get("status"),
		Query:   r.URL.Query().Get("q"),
		Page:    queryInt(r, "page", 1),
		PerPage: queryInt(r, "per_page", 20),
	}
	jobs, err := s.DB.History(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Total count as a header: the list body stays the array the frontend
	// already expects, but pagination now has something to work with. It uses
	// the same filter as the list, or the count would describe a different
	// query than the one that was asked.
	if total, err := s.DB.HistoryCount(filter); err == nil {
		w.Header().Set("X-Total-Count", itoa(total))
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) historyStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.DB.Stats(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ---------------------------------------------------------------- schedules

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.SchedulesByUser(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ProfileID      int64  `json:"profile_id"`
		CronExpression string `json:"cron_expression"`
		IsActive       *bool  `json:"is_active"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if _, err := s.DB.ProfileByID(b.ProfileID, userFrom(r).ID); storeErr(w, err, "Profile not found") {
		return
	}
	if err := runner.ValidateCron(b.CronExpression); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid cron expression: "+err.Error())
		return
	}

	sc := &store.Schedule{
		UserID: userFrom(r).ID, ProfileID: b.ProfileID,
		CronExpression: b.CronExpression, IsActive: boolOr(b.IsActive, true),
	}
	if err := s.DB.CreateSchedule(sc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Cron.Reload()
	sc, _ = s.DB.ScheduleByID(sc.ID, sc.UserID)
	writeJSON(w, http.StatusCreated, sc)
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Schedule not found")
		return
	}
	sc, err := s.DB.ScheduleByID(id, userFrom(r).ID)
	if storeErr(w, err, "Schedule not found") {
		return
	}
	var b struct {
		CronExpression *string `json:"cron_expression"`
		IsActive       *bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.CronExpression != nil {
		if err := runner.ValidateCron(*b.CronExpression); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid cron expression: "+err.Error())
			return
		}
		sc.CronExpression = *b.CronExpression
	}
	if b.IsActive != nil {
		sc.IsActive = *b.IsActive
	}
	if err := s.DB.UpdateSchedule(sc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Cron.Reload()
	sc, _ = s.DB.ScheduleByID(sc.ID, sc.UserID)
	writeJSON(w, http.StatusOK, sc)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Schedule not found")
		return
	}
	if err := s.DB.DeleteSchedule(id, userFrom(r).ID); storeErr(w, err, "Schedule not found") {
		return
	}
	s.Cron.Reload()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ------------------------------------------------------------ notifications

func (s *Server) getNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := s.DB.NotificationSettings(userFrom(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		// The frontend treats a null body as "not configured yet".
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) updateNotifications(w http.ResponseWriter, r *http.Request) {
	var b struct {
		SMTPHost        *string `json:"smtp_host"`
		SMTPPort        *int    `json:"smtp_port"`
		SMTPUser        *string `json:"smtp_user"`
		SMTPPassword    *string `json:"smtp_password"`
		FromEmail       *string `json:"from_email"`
		ToEmail         *string `json:"to_email"`
		NotifyOnSuccess *bool   `json:"notify_on_success"`
		NotifyOnFailure *bool   `json:"notify_on_failure"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}

	n, err := s.DB.NotificationSettings(userFrom(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		n = &store.NotificationSettings{UserID: userFrom(r).ID, SMTPPort: 587, NotifyOnFailure: true}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if b.SMTPHost != nil {
		n.SMTPHost = b.SMTPHost
	}
	if b.SMTPPort != nil {
		n.SMTPPort = *b.SMTPPort
	}
	if b.SMTPUser != nil {
		n.SMTPUser = b.SMTPUser
	}
	if b.FromEmail != nil {
		n.FromEmail = b.FromEmail
	}
	if b.ToEmail != nil {
		n.ToEmail = b.ToEmail
	}
	if b.NotifyOnSuccess != nil {
		n.NotifyOnSuccess = *b.NotifyOnSuccess
	}
	if b.NotifyOnFailure != nil {
		n.NotifyOnFailure = *b.NotifyOnFailure
	}
	// An omitted password keeps the stored one.
	if b.SMTPPassword != nil && *b.SMTPPassword != "" {
		enc, err := s.Sec.Encrypt(*b.SMTPPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n.SMTPPasswordEnc = &enc
	}

	if err := s.DB.SaveNotificationSettings(n); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ = s.DB.NotificationSettings(userFrom(r).ID)
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) testNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := s.DB.NotificationSettings(userFrom(r).ID)
	if err != nil || n.SMTPHost == nil || *n.SMTPHost == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": "SMTP not configured"})
		return
	}
	password := ""
	if n.SMTPPasswordEnc != nil {
		if password, err = s.Sec.Decrypt(*n.SMTPPasswordEnc); err != nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": err.Error()})
			return
		}
	}
	if err := s.Mailer.SendTest(n, password); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func boolOr(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}
