package services

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

// A real Fernet key, for tests only.
const testFernetKey = "0XgJN2Yxp4KXExNTHA6m8nKFQ0sEe_2Qmsje0sYw4Ww="

// argvRecorder stands in for imapsync and prints the arguments it was given, so
// a test can assert on the command line that was actually built.
func argvRecorder(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv-recorder")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho \"ARGV: $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func starter(t *testing.T) (*Starter, *store.DB, *store.User, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	sec, err := security.New("test-secret", testFernetKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.Mkdir(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	run := runner.New(argvRecorder(t), dir, dir, logDir)

	u := &store.User{Username: "u", Email: "u@x.com", HashedPassword: "h", IsAdmin: true}
	if err := db.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	return &Starter{DB: db, Sec: sec, Runner: run}, db, u, logDir
}

func profileWithOptions(t *testing.T, s *Starter, db *store.DB, u *store.User, opts store.JSONMap) int64 {
	t.Helper()
	e1, _ := s.Sec.Encrypt("pw1")
	e2, _ := s.Sec.Encrypt("pw2")
	p := &store.Profile{UserID: u.ID, Name: "p",
		Host1: "h1", User1: "u1", Password1Enc: e1,
		Host2: "h2", User2: "u2", Password2Enc: e2,
		SSL1: true, SSL2: true, ExtraOptions: opts}
	if err := db.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func argvFor(t *testing.T, s *Starter, logDir string, jobID int64) string {
	t.Helper()
	if job := s.Runner.Get(jobID); job != nil {
		job.Wait()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(logDir, strconv.FormatInt(jobID, 10)+".txt"))
		if err == nil && strings.Contains(string(b), "ARGV:") {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the recorder never wrote its arguments (last read: %q, %v)", b, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProfileOptionsCanBeOverriddenForOneRun(t *testing.T) {
	// The New Sync screen offers "use cache" and "automap" as checkboxes and
	// showed the choice back on the review screen, but the profile branch
	// dropped extra_options entirely: unticking "use cache" ran a cached sync
	// anyway, and the user was told otherwise.
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	s, db, u, logDir := starter(t)
	pid := profileWithOptions(t, s, db, u, store.JSONMap{"usecache": true})

	jobID, err := s.StartProfile(u.ID, pid, nil, false, map[string]any{"nousecache": true})
	if err != nil {
		t.Fatal(err)
	}
	argv := argvFor(t, s, logDir, jobID)
	if !strings.Contains(argv, "--nousecache") {
		t.Errorf("the override never reached imapsync:\n%s", argv)
	}
}

func TestProfileOptionsApplyWhenNothingOverridesThem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	s, db, u, logDir := starter(t)
	pid := profileWithOptions(t, s, db, u, store.JSONMap{"automap": true})

	jobID, err := s.StartProfile(u.ID, pid, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	argv := argvFor(t, s, logDir, jobID)
	if !strings.Contains(argv, "--automap") {
		t.Errorf("the profile's own options were lost:\n%s", argv)
	}
}

func TestPasswordsNeverAppearInArgv(t *testing.T) {
	// The whole reason writePassfile exists. Worth an assertion at the level
	// that actually builds the command line.
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	s, db, u, logDir := starter(t)
	pid := profileWithOptions(t, s, db, u, nil)

	jobID, err := s.StartProfile(u.ID, pid, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	argv := argvFor(t, s, logDir, jobID)
	for _, secret := range []string{"pw1", "pw2"} {
		if strings.Contains(argv, secret) {
			t.Errorf("a password reached the command line, where ps can read it:\n%s", argv)
		}
	}
	if !strings.Contains(argv, "--passfile1") || !strings.Contains(argv, "--passfile2") {
		t.Errorf("passwords were not passed by file:\n%s", argv)
	}
}
