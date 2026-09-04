package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/imapsync/web-backend/internal/config"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

// A real Fernet key, for tests only.
const testFernetKey = "0XgJN2Yxp4KXExNTHA6m8nKFQ0sEe_2Qmsje0sYw4Ww="

// stubStarter records what it was asked to run and never spawns anything.
type stubStarter struct {
	db           *store.DB
	lastOverride map[string]any
}

// The handler reads back the job the starter created, so the stub has to make
// a real row — just not a real process.
func (s *stubStarter) record(userID int64, profileID *int64) (int64, error) {
	at := store.NaiveTime{Time: store.NowVN()}
	src, dst := "a@x", "b@y"
	j := &store.SyncJob{UserID: userID, ProfileID: profileID, Status: "running",
		StartedAt: &at, SourceAccount: &src, DestAccount: &dst}
	if err := s.db.CreateJob(j); err != nil {
		return 0, err
	}
	return j.ID, nil
}

func (s *stubStarter) StartProfile(userID, profileID int64, folders []string, dryRun bool,
	override map[string]any) (int64, error) {
	// The real one resolves the profile scoped to the user first, and the
	// handler turns ErrNotFound into a 404. A stub that skipped that would make
	// the authorization test pass for the wrong reason.
	if _, err := s.db.ProfileByID(profileID, userID); err != nil {
		return 0, err
	}
	s.lastOverride = override
	return s.record(userID, &profileID)
}

func (s *stubStarter) StartAdhoc(userID int64, p AdhocParams) (int64, error) {
	return s.record(userID, nil)
}

type testEnv struct {
	srv     *Server
	handler http.Handler
	db      *store.DB
	sec     *security.Manager
	starter *stubStarter
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	sec, err := security.New("test-secret-key", testFernetKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	starter := &stubStarter{db: db}
	srv := &Server{
		Cfg:     &config.Config{DataDir: dir, LogDir: dir},
		DB:      db,
		Sec:     sec,
		Runner:  runner.New("/bin/true", dir, dir, dir),
		Starter: starter,
	}
	return &testEnv{srv: srv, handler: srv.Routes(), db: db, sec: sec, starter: starter}
}

// account creates a user and returns a bearer token for them.
func (e *testEnv) account(t *testing.T, name string) (*store.User, string) {
	t.Helper()
	hash, err := security.HashPassword("pw-" + name)
	if err != nil {
		t.Fatal(err)
	}
	u := &store.User{Username: name, Email: name + "@x.com", HashedPassword: hash, IsAdmin: true}
	if err := e.db.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	tok, err := e.sec.CreateToken(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u, tok
}

func (e *testEnv) do(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)

	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *testEnv) profile(t *testing.T, u *store.User, name string) int64 {
	t.Helper()
	enc, _ := e.sec.Encrypt("secret")
	p := &store.Profile{UserID: u.ID, Name: name,
		Host1: "h1", User1: "u1", Password1Enc: enc,
		Host2: "h2", User2: "u2", Password2Enc: enc}
	if err := e.db.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// TestOneUserCannotReachAnothersData walks every route that takes an {id} and
// checks it against an object belonging to somebody else.
//
// Three independent reviews traced this by hand and found it correct. Nothing
// in the test suite held it there, so the next handler added to this file could
// quietly break it. 404 is the right answer rather than 403: whether an id
// exists is itself information.
func TestOneUserCannotReachAnothersData(t *testing.T) {
	e := newTestEnv(t)
	alice, aliceTok := e.account(t, "alice")
	_, bobTok := e.account(t, "bob")

	aliceProfile := e.profile(t, alice, "alice-profile")

	// Something of Alice's in every table Bob might reach for.
	started := store.NaiveTime{Time: store.NowVN()}
	src, dst := "a@x", "b@y"
	job := &store.SyncJob{UserID: alice.ID, Status: "completed", StartedAt: &started,
		SourceAccount: &src, DestAccount: &dst}
	if err := e.db.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	sched := &store.Schedule{UserID: alice.ID, ProfileID: aliceProfile,
		CronExpression: "0 3 * * *", IsActive: true}
	if err := e.db.CreateSchedule(sched); err != nil {
		t.Fatal(err)
	}
	batch := &store.BatchRun{UserID: alice.ID, Filename: "a.csv", TotalRows: 1, Status: "pending"}
	if err := e.db.CreateBatch(batch); err != nil {
		t.Fatal(err)
	}
	key := &store.APIKey{UserID: alice.ID, Name: "k", Prefix: "isk_a", Scopes: "admin", RateLimit: 60}
	if err := e.db.CreateAPIKey(key, "hash-alice"); err != nil {
		t.Fatal(err)
	}
	hook := &store.Webhook{UserID: alice.ID, URL: "https://example.com/h", Events: "*", IsActive: true}
	if err := e.db.CreateWebhook(hook, "sekret"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		method, path string
		body         any
	}{
		{"GET", fmt.Sprintf("/api/profiles/%d", aliceProfile), nil},
		{"PUT", fmt.Sprintf("/api/profiles/%d", aliceProfile), map[string]any{"name": "stolen"}},
		{"DELETE", fmt.Sprintf("/api/profiles/%d", aliceProfile), nil},
		{"GET", fmt.Sprintf("/api/sync/%d", job.ID), nil},
		{"GET", fmt.Sprintf("/api/sync/%d/log", job.ID), nil},
		{"DELETE", fmt.Sprintf("/api/sync/%d", job.ID), nil},
		{"POST", fmt.Sprintf("/api/sync/%d/abort", job.ID), nil},
		{"PUT", fmt.Sprintf("/api/schedules/%d", sched.ID), map[string]any{"is_active": false}},
		{"DELETE", fmt.Sprintf("/api/schedules/%d", sched.ID), nil},
		{"GET", fmt.Sprintf("/api/batch/%d", batch.ID), nil},
		{"POST", fmt.Sprintf("/api/batch/%d/start", batch.ID), nil},
		{"POST", fmt.Sprintf("/api/batch/%d/abort", batch.ID), nil},
		{"POST", fmt.Sprintf("/api/api-keys/%d/revoke", key.ID), nil},
		{"DELETE", fmt.Sprintf("/api/api-keys/%d", key.ID), nil},
		{"PUT", fmt.Sprintf("/api/webhooks/%d", hook.ID), map[string]any{"is_active": false}},
		{"DELETE", fmt.Sprintf("/api/webhooks/%d", hook.ID), nil},
		{"POST", fmt.Sprintf("/api/webhooks/%d/test", hook.ID), nil},
		{"POST", "/api/sync/start", map[string]any{"profile_id": aliceProfile}},
	}

	for _, c := range cases {
		code, body := e.do(t, c.method, c.path, bobTok, c.body)
		if code < 400 {
			t.Errorf("%s %s: Bob got %d — he reached Alice's data (%v)",
				c.method, c.path, code, body)
		}
		if code == http.StatusUnauthorized {
			t.Errorf("%s %s: got 401, but Bob is authenticated — the check is wrong",
				c.method, c.path)
		}
	}

	// And Alice's own things are still reachable by Alice, so the test above is
	// not passing because everything is broken.
	if code, _ := e.do(t, "GET", fmt.Sprintf("/api/profiles/%d", aliceProfile), aliceTok, nil); code != 200 {
		t.Errorf("Alice cannot read her own profile: %d", code)
	}
	if code, _ := e.do(t, "GET", fmt.Sprintf("/api/sync/%d", job.ID), aliceTok, nil); code != 200 {
		t.Errorf("Alice cannot read her own job: %d", code)
	}
}

func TestListsOnlyShowYourOwnRows(t *testing.T) {
	e := newTestEnv(t)
	alice, _ := e.account(t, "alice")
	_, bobTok := e.account(t, "bob")
	e.profile(t, alice, "alice-profile")

	for _, path := range []string{"/api/profiles", "/api/schedules", "/api/history", "/api/sync/running"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+bobTok)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)

		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Errorf("%s: %v (body %s)", path, err, rec.Body.String())
			continue
		}
		if len(rows) != 0 {
			t.Errorf("%s: Bob sees %d of Alice's rows", path, len(rows))
		}
	}
}

func TestUnauthenticatedRequestsAreRefused(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{
		"/api/profiles", "/api/history", "/api/sync/running", "/api/schedules",
		"/api/api-keys", "/api/webhooks", "/api/telegram/settings",
	} {
		if code, _ := e.do(t, "GET", path, "", nil); code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token: %d, want 401", path, code)
		}
	}
	// Health is the one thing that must answer without a token.
	if code, _ := e.do(t, "GET", "/api/health", "", nil); code != 200 {
		t.Errorf("health check needs a token: %d", code)
	}
}

func TestForgedAndExpiredTokensAreRefused(t *testing.T) {
	e := newTestEnv(t)
	u, good := e.account(t, "alice")

	other, err := security.New("a-different-secret", testFernetKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := other.CreateToken(u.ID)

	expiredMgr, err := security.New("test-secret-key", testFernetKey, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	expired, _ := expiredMgr.CreateToken(u.ID)

	for name, tok := range map[string]string{
		"garbage":            "not-a-token",
		"signed elsewhere":   foreign,
		"already expired":    expired,
		"truncated":          good[:len(good)-6],
		"tampered signature": good[:len(good)-4] + "AAAA",
	} {
		if code, _ := e.do(t, "GET", "/api/profiles", tok, nil); code != http.StatusUnauthorized {
			t.Errorf("%s token was accepted: %d", name, code)
		}
	}
}

func TestRegistrationStaysClosed(t *testing.T) {
	// Accounts are made with createadmin. If this ever starts returning 201,
	// anyone on the internet can create an account on a live migration server.
	e := newTestEnv(t)
	code, _ := e.do(t, "POST", "/api/auth/register", "",
		map[string]any{"username": "x", "email": "x@x.com", "password": "12345678"})
	if code != http.StatusForbidden {
		t.Errorf("register returned %d, want 403", code)
	}
}

// apiKey issues a real key for a user and returns the plaintext value.
func (e *testEnv) apiKey(t *testing.T, u *store.User, scopes string, expires *store.NaiveTime) string {
	t.Helper()
	plain, prefix, digest, err := generateKey()
	if err != nil {
		t.Fatal(err)
	}
	k := &store.APIKey{UserID: u.ID, Name: "k-" + scopes, Prefix: prefix,
		Scopes: scopes, RateLimit: 1000, ExpiresAt: expires}
	if err := e.db.CreateAPIKey(k, digest); err != nil {
		t.Fatal(err)
	}
	return plain
}

func (e *testEnv) withKey(t *testing.T, method, path, key string, body any) int {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", key)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code
}

func TestAKeyCannotDoMoreThanItsScopes(t *testing.T) {
	e := newTestEnv(t)
	alice, _ := e.account(t, "alice")
	pid := e.profile(t, alice, "p")

	readOnly := e.apiKey(t, alice, "sync:read", nil)
	if code := e.withKey(t, "GET", "/api/sync/running", readOnly, nil); code != 200 {
		t.Errorf("sync:read could not read running jobs: %d", code)
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/sync/start", map[string]any{"profile_id": pid}}, // sync:write
		{"GET", "/api/profiles", nil},                                  // profiles:read
		{"POST", "/api/batch/upload-text", map[string]any{"csv_text": "a;b;c;d;e;f"}},
		{"GET", "/api/api-keys", nil},                                       // admin
		{"GET", "/api/webhooks", nil},                                       // admin
		{"PUT", "/api/telegram/settings", map[string]any{"enabled": false}}, // admin
	} {
		if code := e.withKey(t, c.method, c.path, readOnly, c.body); code != http.StatusForbidden {
			t.Errorf("a sync:read key reached %s %s: %d, want 403", c.method, c.path, code)
		}
	}
}

func TestAdminScopeIsRequiredToMintMoreKeys(t *testing.T) {
	// Otherwise a narrow key escalates itself by issuing a wider one.
	e := newTestEnv(t)
	alice, _ := e.account(t, "alice")
	narrow := e.apiKey(t, alice, "sync:read,sync:write,profiles:write", nil)

	code := e.withKey(t, "POST", "/api/api-keys", narrow,
		map[string]any{"name": "wider", "scopes": []string{"admin"}})
	if code != http.StatusForbidden {
		t.Errorf("a non-admin key created another key: %d", code)
	}
}

func TestRevokedAndExpiredKeysStopWorking(t *testing.T) {
	e := newTestEnv(t)
	alice, _ := e.account(t, "alice")

	live := e.apiKey(t, alice, "sync:read", nil)
	if code := e.withKey(t, "GET", "/api/sync/running", live, nil); code != 200 {
		t.Fatalf("a fresh key did not work: %d", code)
	}

	// Expiry three hours in the past. This used to authenticate for another
	// four hours: the stored value is a naive UTC+7 reading that the driver
	// returns tagged UTC, and it was compared as an instant.
	past := store.NaiveTime{Time: store.NowVN().Add(-3 * time.Hour)}
	stale := e.apiKey(t, alice, "sync:read", &past)
	if code := e.withKey(t, "GET", "/api/sync/running", stale, nil); code != http.StatusUnauthorized {
		t.Errorf("an expired key still authenticated: %d", code)
	}

	future := store.NaiveTime{Time: store.NowVN().Add(time.Hour)}
	valid := e.apiKey(t, alice, "sync:read", &future)
	if code := e.withKey(t, "GET", "/api/sync/running", valid, nil); code != 200 {
		t.Errorf("a key expiring in an hour was refused: %d", code)
	}
}

func TestOptionsRejectedOnBothStartPaths(t *testing.T) {
	// --pipemess runs a shell command. It has to be refused whether the caller
	// names a profile or passes credentials, and the profile branch used to
	// skip validation entirely because it dropped extra_options.
	e := newTestEnv(t)
	alice, tok := e.account(t, "alice")
	pid := e.profile(t, alice, "p")

	for _, body := range []map[string]any{
		{"profile_id": pid, "extra_options": map[string]any{"pipemess": "curl evil.example"}},
		{"host1": "h", "user1": "u", "password1": "p", "host2": "h", "user2": "u", "password2": "p",
			"extra_options": map[string]any{"pipemess": "curl evil.example"}},
	} {
		code, resp := e.do(t, "POST", "/api/sync/start", tok, body)
		if code != http.StatusBadRequest {
			t.Errorf("pipemess was accepted: %d %v", code, resp)
		}
	}

	// And a legitimate override reaches the starter on the profile path.
	code, _ := e.do(t, "POST", "/api/sync/start", tok,
		map[string]any{"profile_id": pid, "extra_options": map[string]any{"nousecache": true}})
	if code != 200 {
		t.Fatalf("a valid override was refused: %d", code)
	}
	if e.starter.lastOverride["nousecache"] != true {
		t.Errorf("the override never reached the starter: %v", e.starter.lastOverride)
	}
}
