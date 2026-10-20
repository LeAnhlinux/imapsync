package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEndToEnd drives every tool against a real imapsync web server, so a
// wrong path or body shows up as the API's own error rather than passing
// against a fake that agrees with the mistake. It is skipped unless pointed
// at a server:
//
//	IMAPSYNC_E2E_URL=http://127.0.0.1:18080 IMAPSYNC_E2E_KEY=isk_... go test -run EndToEnd
//
// The key needs the admin scope. Use a throwaway instance with IMAPSYNC_PATH
// set to a stub: this creates and deletes profiles, jobs, keys and webhooks.
func TestEndToEnd(t *testing.T) {
	base, key := os.Getenv("IMAPSYNC_E2E_URL"), os.Getenv("IMAPSYNC_E2E_KEY")
	if base == "" || key == "" {
		t.Skip("set IMAPSYNC_E2E_URL and IMAPSYNC_E2E_KEY to run against a real server")
	}
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := newServer(NewClient(base, key), false).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	called := map[string]bool{}
	// run calls a tool, fails the test on an error answer, and decodes the
	// answer into a map when it is an object.
	run := func(name string, args map[string]any) map[string]any {
		t.Helper()
		called[name] = true
		text, isErr := call(t, cs, name, args)
		if isErr {
			t.Errorf("%s: %s", name, text)
			return nil
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m
	}
	// expectErr is for calls whose refusal is the point.
	expectErr := func(name string, args map[string]any, want string) {
		t.Helper()
		called[name] = true
		if text, isErr := call(t, cs, name, args); !isErr || !strings.Contains(text, want) {
			t.Errorf("%s: got %v %q, want an error containing %q", name, isErr, text, want)
		}
	}
	num := func(m map[string]any, k string) float64 { f, _ := m[k].(float64); return f }

	src := map[string]any{"host": "old.example", "user": "a@old.example", "password": "pw-a"}
	dst := map[string]any{"host": "new.example", "user": "b@new.example", "password": "pw-b", "port": 1993}

	run("whoami", nil)

	// ---- profiles
	p := run("create_profile", map[string]any{"name": "e2e", "source": src, "destination": dst,
		"extra_options": map[string]any{"automap": true}})
	pid := num(p, "id")
	if pid == 0 {
		t.Fatalf("create_profile returned no id: %v", p)
	}
	run("list_profiles", nil)
	run("get_profile", map[string]any{"profile_id": pid})
	if up := run("update_profile", map[string]any{"profile_id": pid, "name": "e2e renamed",
		"destination": map[string]any{"ssl": false}}); up["name"] != "e2e renamed" || up["ssl2"] != false {
		t.Errorf("update_profile did not apply: %v", up)
	}
	expectErr("update_profile", map[string]any{"profile_id": pid,
		"extra_options": map[string]any{"delete1": true}}, "refused")

	// ---- folders
	if b := run("browse_folders", map[string]any{"profile_id": pid}); b["status"] != "done" {
		t.Errorf("browse by profile: %v", b)
	}
	run("browse_folders", map[string]any{"mailbox": src})

	// ---- a sync, followed to the end
	job := run("start_sync", map[string]any{"profile_id": pid, "dry_run": true, "folders": []string{"INBOX"}})
	jid := num(job, "id")
	if done := run("get_job", map[string]any{"job_id": jid, "wait_seconds": 60}); done["status"] != "completed" {
		t.Errorf("job did not complete: %v", done)
	}
	if lg := run("get_job_log", map[string]any{"job_id": jid, "tail": 5}); lg["log"] == "" {
		t.Errorf("empty log: %v", lg)
	}
	run("get_job_log", map[string]any{"job_id": jid, "offset": 0})
	expectErr("start_sync", map[string]any{"source": src, "destination": dst,
		"extra_options": map[string]any{"delete2": true}}, "refused")

	// ---- an ad-hoc sync, aborted mid-run
	adhoc := run("start_sync", map[string]any{"source": src, "destination": dst})
	run("list_running_jobs", nil)
	run("abort_job", map[string]any{"job_id": num(adhoc, "id")})
	run("get_job", map[string]any{"job_id": num(adhoc, "id"), "wait_seconds": 30})

	run("list_history", map[string]any{"per_page": 5})
	run("history_stats", nil)
	run("delete_job", map[string]any{"job_id": jid})

	// ---- batches
	csv := "old.example;c@old;pw;new.example;c@new;pw\n"
	expectErr("create_batch", map[string]any{"csv_text": csv[:len(csv)-1] + ";--delete1\n"}, "refused")
	batch := run("create_batch", map[string]any{"csv_text": csv, "dry_run": true})
	bid := num(batch, "id")
	run("start_batch", map[string]any{"batch_id": bid})
	run("get_batch", map[string]any{"batch_id": bid})
	run("abort_batch", map[string]any{"batch_id": bid})

	// ---- schedules
	sch := run("create_schedule", map[string]any{"profile_id": pid, "cron_expression": "0 3 * * *"})
	sid := num(sch, "id")
	run("list_schedules", nil)
	run("update_schedule", map[string]any{"schedule_id": sid, "is_active": false})
	run("delete_schedule", map[string]any{"schedule_id": sid})

	// ---- history upkeep
	run("get_history_retention", nil)
	run("set_history_retention", map[string]any{"enabled": true, "keep_days": 3650})
	run("run_history_retention", nil)
	run("set_history_retention", map[string]any{"enabled": false, "keep_days": 3650})
	run("purge_history", map[string]any{"older_than_days": 3650})

	// ---- notifications
	run("get_email_notifications", nil)
	run("update_email_notifications", map[string]any{"notify_on_failure": true})
	run("test_email_notifications", nil)
	run("get_telegram", nil)
	run("update_telegram", map[string]any{"notify_on_failure": true})
	run("telegram_status", nil)
	run("test_telegram", nil)

	// ---- keys
	run("list_api_keys", nil)
	run("list_api_key_scopes", nil)
	k := run("create_api_key", map[string]any{"name": "e2e-key", "scopes": []string{"sync:read"}})
	if !strings.HasPrefix(k["key"].(string), "isk_") {
		t.Errorf("create_api_key did not return the key: %v", k)
	}
	kid := num(k, "id")
	if uk := run("update_api_key", map[string]any{"key_id": kid, "scopes": []string{"sync:read", "sync:write"}}); uk["scopes"] != "sync:read,sync:write" {
		t.Errorf("update_api_key: %v", uk)
	}
	run("revoke_api_key", map[string]any{"key_id": kid})
	run("delete_api_key", map[string]any{"key_id": kid})

	// ---- webhooks
	run("list_webhook_events", nil)
	wh := run("create_webhook", map[string]any{"url": "http://127.0.0.1:9/e2e"})
	wid := num(wh, "id")
	run("list_webhooks", nil)
	run("update_webhook", map[string]any{"webhook_id": wid, "is_active": false})
	run("test_webhook", map[string]any{"webhook_id": wid})
	run("delete_webhook", map[string]any{"webhook_id": wid})

	run("delete_profile", map[string]any{"profile_id": pid})

	// Every tool the server offers has been through the real API.
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if !called[tool.Name] {
			t.Errorf("tool %s is not exercised by this test", tool.Name)
		}
	}
	t.Logf("%d tools exercised against %s", len(res.Tools), base)
}
