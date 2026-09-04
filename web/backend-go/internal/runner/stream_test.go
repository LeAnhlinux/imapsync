package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

func TestASlowListenerDoesNotStallTheSync(t *testing.T) {
	// OnProgress is wired to a SQLite write and to the Telegram bot, which POSTs
	// synchronously. Calling it from the reader goroutine meant the 64 KiB pipe
	// filled while the POST was in flight and imapsync itself blocked — so a
	// rate-limited Telegram could freeze a migration. The listener now runs on
	// its own goroutine with latest-value-wins.
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.Mkdir(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Enough copy lines to overflow a 64 KiB pipe several times over.
	script := filepath.Join(dir, "chatty")
	body := `#!/bin/sh
i=1
while [ $i -le 4000 ]; do
  echo "msg Borrador/$i {1050}            copied to x/$i  0.5 msgs/s  1 KiB/s 1 KiB copied ETA: Thu  0 s  $((4000-i))/4000 msgs left"
  i=$((i+1))
done
echo "Messages transferred                    : 4000"
echo "Detected 0 errors"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	r := New(script, dir, dir, logDir)
	var calls int32
	r.OnProgress = func(_ int64, _ Progress) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(2 * time.Second) // a listener that has stopped responding
	}

	job, err := r.Start(1, Params{Host1: "h1", User1: "u1", Password1: "p1",
		Host2: "h2", User2: "u2", Password2: "p2"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() { job.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(25 * time.Second):
		t.Fatal("the sync did not finish: a slow progress listener stalled the reader")
	}

	// Every line must still have been read, and the listener must have been
	// called only a handful of times rather than once per line.
	full := r.LogFor(1)
	if !strings.Contains(full, "Borrador/4000") || !strings.Contains(full, "Detected 0 errors") {
		t.Error("output was lost while the listener was busy")
	}
	if n := atomic.LoadInt32(&calls); n > 10 {
		t.Errorf("the listener was called %d times; updates should coalesce", n)
	}
}

func TestSweepTmpRemovesOrphanedPassfiles(t *testing.T) {
	// A crash or a hang left both passfiles on disk holding cleartext mailbox
	// passwords, with nothing to clean them up: the registry of live jobs is in
	// memory and does not survive a restart.
	dir := t.TempDir()
	keep := filepath.Join(dir, "imapsync_cache")
	if err := os.Mkdir(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"imapsync_p1_123456", "imapsync_p2_789012", "imapsync_7.pid", "something-else.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	r := New("/bin/true", dir, dir, dir)
	n, err := r.SweepTmp()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("removed %d files, want 3", n)
	}
	for _, gone := range []string{"imapsync_p1_123456", "imapsync_p2_789012", "imapsync_7.pid"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s survived the sweep", gone)
		}
	}
	// Anything else in the directory must be left alone — the cache lives here.
	if _, err := os.Stat(filepath.Join(dir, "something-else.txt")); err != nil {
		t.Error("the sweep removed an unrelated file")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("the sweep removed the cache directory")
	}
}
