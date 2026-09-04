package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBPath(t *testing.T) {
	// .env carries a SQLAlchemy-style URL, which the Go driver cannot open.
	for _, c := range []struct{ url, want string }{
		{"sqlite:///./data/imapsync.db", "./data/imapsync.db"},
		{"sqlite:////opt/imapsync-web/data/imapsync.db", "/opt/imapsync-web/data/imapsync.db"},
		{"/plain/path.db", "/plain/path.db"},
	} {
		if got := (&Config{DatabaseURL: c.url}).DBPath(); got != c.want {
			t.Errorf("DBPath(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestLoadRejectsDefaultSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// A generated fallback key would make every stored password
	// undecryptable on the next restart, so both must be startup failures.
	for _, c := range []struct{ name, secret, fernet string }{
		{"no secret", "", "0XgJN2Yxp4KXExNTHA6m8nKFQ0sEe_2Qmsje0sYw4Ww="},
		{"default secret", "change-me-in-production", "0XgJN2Yxp4KXExNTHA6m8nKFQ0sEe_2Qmsje0sYw4Ww="},
		{"no fernet key", "a-real-secret", ""},
		{"default fernet key", "a-real-secret", "change-me-in-production"},
	} {
		t.Setenv("SECRET_KEY", c.secret)
		t.Setenv("IMAPSYNC_FERNET_KEY", c.fernet)
		if _, err := Load(); err == nil {
			t.Errorf("%s: Load succeeded, want an error", c.name)
		}
	}
}

func TestLoadCreatesDataDirs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("SECRET_KEY", strings.Repeat("k", 32))
	t.Setenv("IMAPSYNC_FERNET_KEY", "0XgJN2Yxp4KXExNTHA6m8nKFQ0sEe_2Qmsje0sYw4Ww=")
	t.Setenv("DATA_DIR", filepath.Join(dir, "data"))

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{cfg.DataDir, cfg.LogDir, cfg.TmpDir(), cfg.CacheDir()} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("%s was not created", d)
		}
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Error("DataDir is not absolute; relative paths break under systemd")
	}
}

func TestStaticDirEmptyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("STATIC_DIR", filepath.Join(dir, "nope"))
	if got := (&Config{}).StaticDir(); got != "" {
		t.Errorf("StaticDir = %q for a missing directory, want empty", got)
	}
}

func TestLoadRejectsAShortSecretKey(t *testing.T) {
	// The signing key. A short one is brute-forceable offline from any token a
	// user holds, and a forged token reaches every account.
	t.Setenv("IMAPSYNC_FERNET_KEY", "0XgJN2Yxp4KXExNTHA6m8nKFQ0sEe_2Qmsje0sYw4Ww=")
	t.Setenv("DATA_DIR", t.TempDir())

	for _, short := range []string{"x", "hunter2", strings.Repeat("k", 31)} {
		t.Setenv("SECRET_KEY", short)
		if _, err := Load(); err == nil {
			t.Errorf("a %d-character SECRET_KEY was accepted", len(short))
		}
	}
	t.Setenv("SECRET_KEY", strings.Repeat("k", 32))
	if _, err := Load(); err != nil {
		t.Errorf("a 32-character key was refused: %v", err)
	}
}
