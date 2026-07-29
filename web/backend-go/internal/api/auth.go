package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/imapsync/web-backend/internal/security"
)

// Failed logins are throttled per client address and username. In-process
// state is enough: one binary owns the whole API.
type throttle struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

const (
	maxAttempts    = 10
	throttleWindow = 5 * time.Minute
)

var loginThrottle = &throttle{attempts: map[string][]time.Time{}}

func (t *throttle) blocked(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-throttleWindow)
	kept := t.attempts[key][:0]
	for _, at := range t.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	t.attempts[key] = kept
	return len(kept) >= maxAttempts
}

func (t *throttle) record(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.attempts[key] = append(t.attempts[key], time.Now())
}

func (t *throttle) clear(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, key)
}

func clientIP(r *http.Request) string {
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}

// authenticate resolves the bearer token into the user for the request.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "Invalid token")
			return
		}
		userID, err := s.Sec.UserIDFromToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Invalid token")
			return
		}
		user, err := s.DB.UserByID(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "User not found")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func (s *Server) register(w http.ResponseWriter, _ *http.Request) {
	// Accounts are created with the createadmin command, not over the network.
	writeError(w, http.StatusForbidden, "Registration is disabled")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	key := clientIP(r) + ":" + body.Username
	if loginThrottle.blocked(key) {
		writeError(w, http.StatusTooManyRequests,
			"Too many failed login attempts. Try again in a few minutes.")
		return
	}

	user, err := s.DB.UserByUsername(body.Username)
	if err != nil || !security.VerifyPassword(body.Password, user.HashedPassword) {
		loginThrottle.record(key)
		writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}
	loginThrottle.clear(key)

	token, err := s.Sec.CreateToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": token,
		"token_type":   "bearer",
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r))
}
