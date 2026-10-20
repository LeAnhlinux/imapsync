package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/imapsync/web-backend/internal/store"
)

// KeyPrefix marks a credential as an API key rather than a session JWT, so one
// Authorization header can carry either.
const KeyPrefix = "isk_"

// Scopes an API key can carry. Coarse on purpose: a key is issued to a script,
// and a script's job is usually "start syncs" or "read status", not one route.
var ValidScopes = []string{
	"sync:read",     // list and inspect jobs, read logs and history
	"sync:write",    // start and abort syncs
	"profiles:read", // list profiles
	"profiles:write",
	"batch:write", // upload and run batches
	"schedules:read",
	"schedules:write",
	"admin", // everything, including settings
}

func validScope(s string) bool {
	for _, v := range ValidScopes {
		if v == s {
			return true
		}
	}
	return false
}

// generateKey returns the key to show the user once, plus what gets stored.
func generateKey() (key, prefix, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	key = KeyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(key))
	// The prefix is stored in the clear so a key can be recognised in a list
	// without being able to reconstruct it.
	return key, key[:12], hex.EncodeToString(sum[:]), nil
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- rate limit

// Fixed one-minute windows per key. A token bucket would smooth bursts better,
// but a window is what the X-RateLimit headers describe, and matching the
// headers to the behaviour is worth more here than smoothing.
type rateLimiter struct {
	mu      sync.Mutex
	windows map[int64]*window
}

type window struct {
	count int
	until time.Time
}

var apiKeyLimiter = &rateLimiter{windows: map[int64]*window{}}

func (rl *rateLimiter) take(keyID int64, limit int) (remaining int, reset time.Time, ok bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w := rl.windows[keyID]
	if w == nil || now.After(w.until) {
		w = &window{until: now.Add(time.Minute)}
		rl.windows[keyID] = w
	}
	if w.count >= limit {
		return 0, w.until, false
	}
	w.count++
	return limit - w.count, w.until, true
}

// ---------------------------------------------------------------- middleware

const apiKeyCtxKey ctxKey = "apikey"

func apiKeyFrom(r *http.Request) *store.APIKey {
	k, _ := r.Context().Value(apiKeyCtxKey).(*store.APIKey)
	return k
}

// requireScope guards a route when the caller authenticated with an API key.
// A browser session (JWT) is not scope-limited: it already went through login.
func requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key := apiKeyFrom(r); key != nil && !key.Allows(scope) {
				writeError(w, http.StatusForbidden,
					fmt.Sprintf("This API key does not carry the %q scope", scope))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ------------------------------------------------------------------ handlers

type apiKeyCreated struct {
	*store.APIKey
	// Key is present exactly once, in the response to the create call.
	Key string `json:"key"`
}

// checkScopes returns why a scope list is unacceptable, or "" if it is fine.
func checkScopes(scopes []string) string {
	if len(scopes) == 0 {
		return "Pick at least one scope"
	}
	for _, sc := range scopes {
		if !validScope(sc) {
			return fmt.Sprintf("Unknown scope %q. Valid: %s", sc, strings.Join(ValidScopes, ", "))
		}
	}
	return ""
}

func checkRateLimit(n int) string {
	if n < 1 || n > 10000 {
		return "rate_limit_per_minute must be between 1 and 10000"
	}
	return ""
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.DB.APIKeysByUser(userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		RateLimit int      `json:"rate_limit_per_minute"`
		ExpiresIn int      `json:"expires_in_days"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeError(w, http.StatusBadRequest, "A name is required, so you can tell your keys apart later")
		return
	}
	if msg := checkScopes(b.Scopes); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if b.RateLimit <= 0 {
		b.RateLimit = 60
	}
	if msg := checkRateLimit(b.RateLimit); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	key, prefix, hash, err := generateKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rec := &store.APIKey{
		UserID: userFrom(r).ID, Name: strings.TrimSpace(b.Name),
		Prefix: prefix, Scopes: strings.Join(b.Scopes, ","), RateLimit: b.RateLimit,
	}
	if b.ExpiresIn > 0 {
		exp := store.NaiveTime{Time: store.NowVN().AddDate(0, 0, b.ExpiresIn)}
		rec.ExpiresAt = &exp
	}
	if err := s.DB.CreateAPIKey(rec, hash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The only time the key is ever returned. It is not recoverable after this.
	writeJSON(w, http.StatusCreated, apiKeyCreated{APIKey: rec, Key: key})
}

// updateAPIKey changes a key's name, scopes or rate limit in place, so
// widening or narrowing what an integration may do does not mean issuing a new
// key and redeploying it. Fields left out of the body are kept.
func (s *Server) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "API key not found")
		return
	}
	key, err := s.DB.APIKeyByID(id, userFrom(r).ID)
	if storeErr(w, err, "API key not found") {
		return
	}
	if key.Revoked {
		writeError(w, http.StatusConflict, "This key has been revoked; create a new one instead")
		return
	}
	var b struct {
		Name      *string  `json:"name"`
		Scopes    []string `json:"scopes"`
		RateLimit *int     `json:"rate_limit_per_minute"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.Name != nil {
		if strings.TrimSpace(*b.Name) == "" {
			writeError(w, http.StatusBadRequest, "A name is required, so you can tell your keys apart later")
			return
		}
		key.Name = strings.TrimSpace(*b.Name)
	}
	if b.Scopes != nil {
		if msg := checkScopes(b.Scopes); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		key.Scopes = strings.Join(b.Scopes, ",")
	}
	if b.RateLimit != nil {
		if msg := checkRateLimit(*b.RateLimit); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		key.RateLimit = *b.RateLimit
	}
	if err := s.DB.UpdateAPIKey(key); storeErr(w, err, "API key not found") {
		return
	}
	writeJSON(w, http.StatusOK, key)
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "API key not found")
		return
	}
	if err := s.DB.RevokeAPIKey(id, userFrom(r).ID); storeErr(w, err, "API key not found") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "API key not found")
		return
	}
	if err := s.DB.DeleteAPIKey(id, userFrom(r).ID); storeErr(w, err, "API key not found") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) listScopes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ValidScopes)
}
