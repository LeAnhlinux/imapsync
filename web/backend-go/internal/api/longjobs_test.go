package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSliceLogTail(t *testing.T) {
	text := "a\nb\nc\n"
	for _, c := range []struct {
		tail int
		want string
	}{
		{1, "c\n"}, {2, "b\nc\n"}, {3, text}, {10, text},
	} {
		got, next := sliceLog(text, 0, c.tail)
		if got != c.want || next != len(text) {
			t.Errorf("tail=%d: got %q next %d, want %q next %d", c.tail, got, next, c.want, len(text))
		}
	}
	if got, _ := sliceLog("a\nb", 0, 1); got != "b" {
		t.Errorf("tail of a log with no trailing newline: %q", got)
	}
}

func TestSliceLogPagesOnLineBoundaries(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	text := strings.Repeat(line, 5000) // ~500 KB, two pages and a bit

	var got strings.Builder
	offset, reads := 0, 0
	for offset < len(text) {
		part, next := sliceLog(text, offset, 0)
		if len(part) > maxLogPage {
			t.Fatalf("page of %d bytes exceeds %d", len(part), maxLogPage)
		}
		if !strings.HasSuffix(part, "\n") {
			t.Fatalf("page at %d ends mid-line", offset)
		}
		got.WriteString(part)
		offset = next
		reads++
	}
	if got.String() != text {
		t.Error("paging through the log did not reassemble it")
	}
	if reads < 2 {
		t.Errorf("a %d-byte log came back in %d read(s)", len(text), reads)
	}
	if part, next := sliceLog(text, len(text)+50, 0); part != "" || next != len(text) {
		t.Errorf("offset past the end: %q, next %d", part, next)
	}
}

func TestSyncLogEndpoint(t *testing.T) {
	e := newTestEnv(t)
	u, tok := e.account(t, "alice")
	jobID, _ := e.starter.record(u.ID, nil)
	text := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(e.srv.Runner.LogDir, fmt.Sprintf("%d.txt", jobID)),
		[]byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	_, all := e.do(t, "GET", fmt.Sprintf("/api/sync/%d/log", jobID), tok, nil)
	if all["log"] != text || all["size"] != float64(len(text)) {
		t.Errorf("without parameters the whole log is returned: %v", all)
	}
	_, tail := e.do(t, "GET", fmt.Sprintf("/api/sync/%d/log?tail=1", jobID), tok, nil)
	if tail["log"] != "line3\n" || tail["next_offset"] != float64(len(text)) {
		t.Errorf("tail=1: %v", tail)
	}
	_, from := e.do(t, "GET", fmt.Sprintf("/api/sync/%d/log?offset=6", jobID), tok, nil)
	if from["log"] != "line2\nline3\n" {
		t.Errorf("offset=6: %v", from)
	}
}

func TestGetSyncWaitsForTheJobToStop(t *testing.T) {
	e := newTestEnv(t)
	u, tok := e.account(t, "alice")
	jobID, _ := e.starter.record(u.ID, nil)

	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = e.db.MarkAborted(jobID)
	}()
	began := time.Now()
	code, job := e.do(t, "GET", fmt.Sprintf("/api/sync/%d?wait=10", jobID), tok, nil)
	if code != 200 || job["status"] != "aborted" {
		t.Fatalf("got %d %v, want the aborted job", code, job)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Errorf("returned after %s, should have answered once the job stopped", took)
	}

	// A job that has already stopped answers at once.
	began = time.Now()
	e.do(t, "GET", fmt.Sprintf("/api/sync/%d?wait=10", jobID), tok, nil)
	if took := time.Since(began); took > time.Second {
		t.Errorf("waiting on a finished job took %s", took)
	}
}

func TestGetSyncWaitGivesUpAtTheDeadline(t *testing.T) {
	e := newTestEnv(t)
	u, tok := e.account(t, "alice")
	jobID, _ := e.starter.record(u.ID, nil)

	_, job := e.do(t, "GET", fmt.Sprintf("/api/sync/%d?wait=1", jobID), tok, nil)
	if job["status"] != "running" {
		t.Errorf("after the wait ran out: %v, want still running", job)
	}
}

func TestBrowseTasks(t *testing.T) {
	b := &browseTasks{tasks: map[string]*browseTask{}}
	release := make(chan struct{})
	calls := 0
	list := func() ([]FolderInfo, error) {
		calls++
		<-release
		return []FolderInfo{{Name: "INBOX", Messages: 3}}, nil
	}

	first := b.start(1, "me@host", list)
	again := b.start(1, "me@host", list)
	if again.ID != first.ID {
		t.Error("a second listing of the same mailbox started while the first was running")
	}
	if _, ok := b.get(2, first.ID); ok {
		t.Error("another user could read the listing")
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, ok := b.get(1, first.ID)
		if !ok {
			t.Fatal("task vanished")
		}
		if got.Status == "done" {
			if len(got.Folders) != 1 || got.Folders[0].Name != "INBOX" {
				t.Errorf("folders: %+v", got.Folders)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls != 1 {
		t.Errorf("list ran %d times, want 1", calls)
	}
}
