package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestThrottleDoesNotRetainUnknownKeys(t *testing.T) {
	// blocked() used to write an entry for every key it was asked about, and the
	// key holds the submitted username. Unauthenticated callers could therefore
	// make the process retain arbitrary memory, permanently: nothing but a
	// successful login for that exact key ever deleted it.
	tr := &throttle{attempts: map[string][]time.Time{}}
	for i := 0; i < 1000; i++ {
		tr.blocked("1.2.3.4:" + strings.Repeat("x", 64))
	}
	if n := len(tr.attempts); n != 0 {
		t.Errorf("%d entries retained for logins that never happened", n)
	}
}

func TestThrottleForgetsExpiredAttempts(t *testing.T) {
	tr := &throttle{attempts: map[string][]time.Time{}}
	key := "1.2.3.4:bob"
	for i := 0; i < maxAttempts; i++ {
		tr.record(key)
	}
	if !tr.blocked(key) {
		t.Fatal("ten failures in the window did not block")
	}
	// Age them past the window and the entry should disappear entirely.
	tr.mu.Lock()
	for i := range tr.attempts[key] {
		tr.attempts[key][i] = time.Now().Add(-2 * throttleWindow)
	}
	tr.mu.Unlock()

	if tr.blocked(key) {
		t.Error("still blocked after the window passed")
	}
	if _, ok := tr.attempts[key]; ok {
		t.Error("the expired entry was kept instead of dropped")
	}
}

func TestClientIPHandlesIPv6(t *testing.T) {
	for addr, want := range map[string]string{
		"1.2.3.4:5678":       "1.2.3.4",
		"[::1]:443":          "::1",
		"[2001:db8::1]:5678": "2001:db8::1",
		"no-port-here":       "no-port-here",
	} {
		if got := clientIP(&http.Request{RemoteAddr: addr}); got != want {
			t.Errorf("clientIP(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestCSVUploadLimitMatchesTheProxy(t *testing.T) {
	// nginx allows 20M so a 16 MiB body plus multipart overhead fits; the
	// handler must refuse anything past its own limit rather than truncate.
	// If these ever drift apart again, a large batch loses mailboxes quietly.
	if maxCSVUpload != 16<<20 {
		t.Errorf("maxCSVUpload is %d; the nginx client_max_body_size comment says 16 MiB", maxCSVUpload)
	}
	if maxJSONBody >= maxCSVUpload {
		t.Error("the JSON body cap should be well under the CSV cap")
	}
}

func TestAnOversizedBodyIsRefusedCleanly(t *testing.T) {
	// The point is a 413 the caller can act on, not a reset connection that the
	// proxy reports as 502.
	e := newTestEnv(t)
	big := strings.Repeat("x", maxJSONBody+1024)
	body := `{"username":"` + big + `","password":"x"}`

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got %d, want 413", rec.Code)
	}
	// And the throttle must not have retained the giant username.
	loginThrottle.mu.Lock()
	n := len(loginThrottle.attempts)
	loginThrottle.mu.Unlock()
	if n != 0 {
		t.Errorf("the throttle kept %d entries from a rejected request", n)
	}
}
