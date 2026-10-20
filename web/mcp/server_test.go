package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeAPI stands in for the imapsync web server and records what was asked.
type fakeAPI struct {
	mu            sync.Mutex
	requests      []string
	started       []map[string]any
	profileOpts   map[string]any
	browsePolls   int
	browseDoneAt  int
	lastKeyHeader string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	f.lastKeyHeader = r.Header.Get("X-API-Key")
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == "GET" && r.URL.Path == "/api/profiles":
		json.NewEncoder(w).Encode([]map[string]any{{
			"id": 1, "name": "Company A", "host1": "old.example", "user1": "me@old",
			"host2": "new.example", "user2": "me@new", "extra_options": map[string]any{"automap": true},
		}})
	case r.Method == "GET" && r.URL.Path == "/api/profiles/1":
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "extra_options": f.profileOpts})
	case r.Method == "POST" && r.URL.Path == "/api/sync/start":
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		f.started = append(f.started, b)
		json.NewEncoder(w).Encode(map[string]any{"id": 7, "status": "running"})
	case r.Method == "GET" && r.URL.Path == "/api/sync/7":
		json.NewEncoder(w).Encode(map[string]any{"id": 7, "status": "completed"})
	case r.Method == "GET" && r.URL.Path == "/api/sync/7/log":
		json.NewEncoder(w).Encode(map[string]any{"log": "last line\n", "size": 10, "next_offset": 10})
	case r.Method == "GET" && r.URL.Path == "/api/sync/404":
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"detail": "Job not found"})
	case r.Method == "POST" && r.URL.Path == "/api/folders/browse":
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{"id": "t1", "status": "running"})
	case r.Method == "GET" && r.URL.Path == "/api/folders/browse/t1":
		f.browsePolls++
		if f.browsePolls >= f.browseDoneAt {
			json.NewEncoder(w).Encode(map[string]any{"id": "t1", "status": "done",
				"folders": []map[string]any{{"name": "INBOX", "messages": 3, "size": 100}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "t1", "status": "running"})
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"detail": "no route " + r.URL.Path})
	}
}

func (f *fakeAPI) saw(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

// connect runs the MCP server against fake over an in-memory transport and
// returns a client session to it — the same path a real MCP client takes.
func connect(t *testing.T, fake *fakeAPI, allowDelete bool) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)

	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	server := newServer(NewClient(ts.URL, "isk_test"), allowDelete)
	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func TestToolsAreListed(t *testing.T) {
	cs := connect(t, &fakeAPI{}, false)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"list_profiles", "browse_folders", "start_sync", "get_job",
		"get_job_log", "list_running_jobs", "abort_job", "list_history", "history_stats"} {
		if !got[want] {
			t.Errorf("tool %s missing", want)
		}
	}
}

func TestListProfilesSendsTheKeyAndSummarises(t *testing.T) {
	fake := &fakeAPI{}
	cs := connect(t, fake, false)
	text, isErr := call(t, cs, "list_profiles", nil)
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "me@old @ old.example") || !strings.Contains(text, "Company A") {
		t.Errorf("profile summary: %s", text)
	}
	if fake.lastKeyHeader != "isk_test" {
		t.Errorf("X-API-Key = %q", fake.lastKeyHeader)
	}
}

func TestStartSyncRefusesDeletingOptions(t *testing.T) {
	fake := &fakeAPI{}
	cs := connect(t, fake, false)

	text, isErr := call(t, cs, "start_sync", map[string]any{
		"profile_id": 1, "extra_options": map[string]any{"delete1": true}})
	if !isErr || !strings.Contains(text, "delete1") {
		t.Errorf("delete1 in the arguments was not refused: %v %s", isErr, text)
	}

	// Saved on the profile, it applies to the run just the same.
	fake.profileOpts = map[string]any{"expunge2": "1"}
	text, isErr = call(t, cs, "start_sync", map[string]any{"profile_id": 1})
	if !isErr || !strings.Contains(text, "expunge2") {
		t.Errorf("expunge2 on the profile was not refused: %v %s", isErr, text)
	}

	// Explicitly switched off is not a deletion.
	fake.profileOpts = map[string]any{"delete2": false}
	if text, isErr = call(t, cs, "start_sync", map[string]any{"profile_id": 1}); isErr {
		t.Errorf("delete2:false was refused: %s", text)
	}

	// A dry run writes nothing, so it may carry them.
	fake.profileOpts = map[string]any{"delete2": true}
	if text, isErr = call(t, cs, "start_sync", map[string]any{"profile_id": 1, "dry_run": true}); isErr {
		t.Errorf("dry run was refused: %s", text)
	}

	if len(fake.started) != 2 {
		t.Errorf("%d syncs reached the API, want 2", len(fake.started))
	}
}

func TestStartSyncAllowsDeletingWhenConfigured(t *testing.T) {
	fake := &fakeAPI{}
	cs := connect(t, fake, true)
	text, isErr := call(t, cs, "start_sync", map[string]any{
		"profile_id": 1, "folders": []string{"INBOX"}, "extra_options": map[string]any{"delete1": true}})
	if isErr {
		t.Fatal(text)
	}
	b := fake.started[0]
	if b["profile_id"] != float64(1) || b["dry_run"] != false {
		t.Errorf("start body: %v", b)
	}
	if fs, _ := b["folders"].([]any); len(fs) != 1 || fs[0] != "INBOX" {
		t.Errorf("folders not passed: %v", b["folders"])
	}
}

func TestGetJobPassesWaitAndCapsIt(t *testing.T) {
	fake := &fakeAPI{}
	cs := connect(t, fake, false)
	text, isErr := call(t, cs, "get_job", map[string]any{"job_id": 7, "wait_seconds": 500})
	if isErr || !strings.Contains(text, "completed") {
		t.Fatalf("%v %s", isErr, text)
	}
	if !fake.saw("GET /api/sync/7?wait=120") {
		t.Errorf("wait was not capped at 120: %v", fake.requests)
	}
}

func TestGetJobLogDefaultsToATail(t *testing.T) {
	fake := &fakeAPI{}
	cs := connect(t, fake, false)
	call(t, cs, "get_job_log", map[string]any{"job_id": 7})
	call(t, cs, "get_job_log", map[string]any{"job_id": 7, "offset": 0})
	if !fake.saw("GET /api/sync/7/log?tail=100") {
		t.Errorf("default read was not a tail: %v", fake.requests)
	}
	if !fake.saw("GET /api/sync/7/log?offset=0") {
		t.Errorf("offset 0 was not passed through: %v", fake.requests)
	}
}

func TestAPIErrorsReachTheModel(t *testing.T) {
	cs := connect(t, &fakeAPI{}, false)
	text, isErr := call(t, cs, "get_job", map[string]any{"job_id": 404})
	if !isErr || !strings.Contains(text, "Job not found") {
		t.Errorf("got %v %q, want an error carrying the server's message", isErr, text)
	}
}

func TestBrowseFoldersPollsUntilDone(t *testing.T) {
	browsePollEvery = 10 * time.Millisecond
	t.Cleanup(func() { browsePollEvery = 3 * time.Second })

	fake := &fakeAPI{browseDoneAt: 3}
	cs := connect(t, fake, false)
	text, isErr := call(t, cs, "browse_folders", map[string]any{"profile_id": 1})
	if isErr || !strings.Contains(text, "INBOX") {
		t.Fatalf("%v %s", isErr, text)
	}
}

func TestBrowseFoldersHandsBackRunning(t *testing.T) {
	browsePollEvery, browseHold = 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { browsePollEvery, browseHold = 3*time.Second, 45*time.Second })

	fake := &fakeAPI{browseDoneAt: 1 << 30}
	cs := connect(t, fake, false)
	text, isErr := call(t, cs, "browse_folders", map[string]any{"profile_id": 1})
	if isErr || !strings.Contains(text, `"running"`) || !strings.Contains(text, "browse_folders again") {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestNewClientAcceptsEitherBase(t *testing.T) {
	for _, in := range []string{"https://x.example", "https://x.example/", "https://x.example/api", "https://x.example/api/"} {
		if got := NewClient(in, "k").base; got != "https://x.example/api" {
			t.Errorf("%q → %q", in, got)
		}
	}
}
