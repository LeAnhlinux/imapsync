package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
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
	// Drop the entry rather than storing an empty slice. The key contains the
	// submitted username, so writing one unconditionally meant every login
	// attempt -- including a first one with a username nobody has ever used --
	// left a permanent map entry that only a *successful* login could remove.
	if len(kept) == 0 {
		delete(t.attempts, key)
		return false
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
	// net.SplitHostPort, not strings.Cut: an IPv6 peer is "[::1]:443", and
	// cutting at the first colon returned "[".
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// maxUsernameLen bounds what goes into a throttle key. Without it the key is
// as long as the submitted username, which nginx allows up to its 50 MB body
// limit.
const maxUsernameLen = 64

// authenticate resolves the caller's credential into a user.
//
// Two kinds are accepted on the same header: a session JWT from the browser,
// and a long-lived API key for programs. They are told apart by the key's
// prefix, so an integration does not need a different endpoint or a login
// round-trip.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential := r.Header.Get("X-API-Key")
		if credential == "" {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				writeError(w, http.StatusUnauthorized, "Invalid token")
				return
			}
			credential = token
		}

		ctx := r.Context()
		var user *store.User

		if strings.HasPrefix(credential, KeyPrefix) {
			key, err := s.DB.APIKeyByHash(hashKey(credential))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "Invalid API key")
				return
			}
			if !key.Usable() {
				writeError(w, http.StatusUnauthorized, "This API key has been revoked or has expired")
				return
			}

			remaining, reset, allowed := apiKeyLimiter.take(key.ID, key.RateLimit)
			w.Header().Set("X-RateLimit-Limit", itoa(key.RateLimit))
			w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", itoa(int(reset.Unix())))
			if !allowed {
				w.Header().Set("Retry-After", itoa(int(time.Until(reset).Seconds())+1))
				writeError(w, http.StatusTooManyRequests,
					"Rate limit exceeded for this API key")
				return
			}

			if user, err = s.DB.UserByID(key.UserID); err != nil {
				writeError(w, http.StatusUnauthorized, "User not found")
				return
			}
			// Best-effort: a failed timestamp update must not fail the request.
			go func() { _ = s.DB.TouchAPIKey(key.ID) }()
			ctx = context.WithValue(ctx, apiKeyCtxKey, key)
		} else {
			userID, err := s.Sec.UserIDFromToken(credential)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "Invalid token")
				return
			}
			if user, err = s.DB.UserByID(userID); err != nil {
				writeError(w, http.StatusUnauthorized, "User not found")
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userKey, user)))
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

	if len(body.Username) > maxUsernameLen {
		// Refuse before the username reaches a throttle key or a query.
		writeError(w, http.StatusUnauthorized, "Invalid credentials")
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
