package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeImapsync writes a script that stands in for imapsync: it prints the first
// half of a transfer, blocks until the test releases it, then prints the rest.
// The pause is what makes "is the log readable while the job is running?"
// answerable without racing.
func fakeImapsync(t *testing.T, gate string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-imapsync")
	script := `#!/bin/sh
echo "Here is imapsync 2.229 on host test"
echo "Host1 folder    1/2 [Borrador]                          Size:     16664 Messages:     7 Biggest:     11379"
echo "msg Borrador/1 {1050}            copied to claudeprobe/1          0.26 msgs/s  0.271 KiB/s 1.025 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:40 +0700 +07  23 s  6/7 msgs left"
while [ ! -f "` + gate + `" ]; do sleep 0.05; done
echo "msg Borrador/6 {11379}           copied to claudeprobe/6          0.73 msgs/s  1.853 KiB/s 15.299 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:23 +0700 +07  1 s  0/7 msgs left"
echo "Messages transferred                    : 2"
echo "Total bytes transferred                 : 12429 (12.138 KiB)"
echo "Detected 0 errors"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLogIsReadableWhileTheJobRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	gate := filepath.Join(dir, "gate")
	logDir := filepath.Join(dir, "logs")
	if err := os.Mkdir(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	r := New(fakeImapsync(t, gate), dir, dir, logDir)

	var mu sync.Mutex
	var last Progress
	r.OnProgress = func(_ int64, p Progress) {
		mu.Lock()
		last = p
		mu.Unlock()
	}

	job, err := r.Start(1, Params{Host1: "h1", User1: "u1", Password1: "p1",
		Host2: "h2", User2: "u2", Password2: "p2"})
	if err != nil {
		t.Fatal(err)
	}

	// The log used to be written in one go after the process exited, so a job
	// that ran for hours had no log to show at all.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if strings.Contains(r.LogFor(1), "Borrador/1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the first copy line never reached the log file; got:\n%s", r.LogFor(1))
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := os.Create(gate); err != nil {
		t.Fatal(err)
	}
	job.Wait()
	// Wait blocks on the process, and the final callback fires just after it.
	time.Sleep(200 * time.Millisecond)

	full := r.LogFor(1)
	for _, want := range []string{"Here is imapsync", "Borrador/1", "Borrador/6", "Detected 0 errors"} {
		if !strings.Contains(full, want) {
			t.Errorf("the saved log is missing %q", want)
		}
	}

	mu.Lock()
	got := last
	mu.Unlock()
	if got.Messages != 2 || got.Bytes != 1050+11379 {
		t.Errorf("final progress is %d msgs / %d bytes, want 2 / 12429", got.Messages, got.Bytes)
	}
	if got.Percent != 100 {
		t.Errorf("final percent is %v, want 100", got.Percent)
	}
}

func TestPassfilesAreRemovedAndNeverInArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	gate := filepath.Join(dir, "gate")
	if err := os.WriteFile(gate, nil, 0o600); err != nil { // never blocks
		t.Fatal(err)
	}
	r := New(fakeImapsync(t, gate), dir, dir, dir)

	job, err := r.Start(2, Params{Host1: "h1", User1: "u1", Password1: "hunter2",
		Host2: "h2", User2: "u2", Password2: "hunter3"})
	if err != nil {
		t.Fatal(err)
	}
	job.Wait()
	time.Sleep(200 * time.Millisecond)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "imapsync_p") {
			t.Errorf("a passfile outlived the process: %s", e.Name())
		}
	}
	if strings.Contains(strings.Join(job.cmd.Args, " "), "hunter2") {
		t.Error("a password was passed in argv, where ps can read it")
	}
}

func TestAnEnormousLineDoesNotHangTheJob(t *testing.T) {
	// A bufio.Scanner stops at its token limit and sets an error nobody read, so
	// the loop ended while imapsync was still writing. cmd.Wait() then blocked
	// forever on a full pipe: the job never finished, the batch behind it never
	// moved, and two passfiles holding cleartext mailbox passwords stayed on
	// disk. This test hangs on the old code and passes on the new.
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.Mkdir(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(dir, "huge-line")
	body := `#!/bin/sh
echo "Here is imapsync 2.229 on host test"
head -c 3000000 /dev/zero | tr '\\0' 'A'
echo
echo "msg Borrador/1 {1050}            copied to x/1  0.5 msgs/s  1 KiB/s 1 KiB copied ETA: Thu  0 s  0/1 msgs left"
echo "Messages transferred                    : 1"
echo "Detected 0 errors"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	r := New(script, dir, dir, logDir)
	job, err := r.Start(1, Params{Host1: "h1", User1: "u1", Password1: "p1",
		Host2: "h2", User2: "u2", Password2: "p2"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() { job.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the job never finished: a single over-long line stalled the reader")
	}
	time.Sleep(200 * time.Millisecond)

	// Everything after the huge line must still have been read.
	full := r.LogFor(1)
	for _, want := range []string{"Here is imapsync", "Borrador/1", "Detected 0 errors"} {
		if !strings.Contains(full, want) {
			t.Errorf("output after the over-long line was lost: missing %q", want)
		}
	}
	if !strings.Contains(full, "[line truncated]") {
		t.Error("the over-long line was kept whole, or silently dropped without a marker")
	}
	// And the truncation must be bounded, not the full 3 MB.
	if len(full) > 4*maxLineLen {
		t.Errorf("the log kept %d bytes; the over-long line was not truncated", len(full))
	}

	// The passfiles must be gone, which is the part that matters most.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "imapsync_p") {
			t.Errorf("a cleartext passfile survived: %s", e.Name())
		}
	}
}
