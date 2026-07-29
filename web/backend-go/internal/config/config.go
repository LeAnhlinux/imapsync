// Package config loads settings from the environment and .env, mirroring what
// the Python backend read so the same .env file drives either one.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	SecretKey    string
	FernetKey    string
	DatabaseURL  string
	ImapsyncPath string
	Domain       string
	Listen       string

	AccessTokenTTL time.Duration

	DataDir string
	LogDir  string
}

// Load reads .env (if present) and the environment. It fails rather than
// falling back on defaults for the two secrets: a generated Fernet key would
// make every stored password undecryptable on the next restart, and a default
// signing key would let anyone mint a valid token.
func Load() (*Config, error) {
	_ = godotenv.Load() // absent .env is not an error; real deploys use the environment

	c := &Config{
		SecretKey:      os.Getenv("SECRET_KEY"),
		FernetKey:      os.Getenv("IMAPSYNC_FERNET_KEY"),
		DatabaseURL:    envOr("DATABASE_URL", "sqlite:///./data/imapsync.db"),
		ImapsyncPath:   envOr("IMAPSYNC_PATH", "/usr/bin/imapsync"),
		Domain:         envOr("DOMAIN", "localhost"),
		Listen:         envOr("LISTEN", "127.0.0.1:8000"),
		AccessTokenTTL: time.Duration(envInt("ACCESS_TOKEN_EXPIRE_MINUTES", 60*24)) * time.Minute,
	}

	if c.SecretKey == "" || c.SecretKey == "change-me-in-production" {
		return nil, fmt.Errorf("SECRET_KEY is unset or still the default. Generate one with: openssl rand -hex 32")
	}
	if c.FernetKey == "" || c.FernetKey == "change-me-in-production" {
		return nil, fmt.Errorf("IMAPSYNC_FERNET_KEY is unset or still the default. Generate one with: " +
			`python3 -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"`)
	}

	dataDir, err := filepath.Abs(envOr("DATA_DIR", "./data"))
	if err != nil {
		return nil, err
	}
	c.DataDir = dataDir
	c.LogDir = filepath.Join(dataDir, "logs")

	for _, dir := range []string{c.DataDir, c.LogDir, c.TmpDir(), c.CacheDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// StaticDir is the built frontend, served when it is present. Empty means
// API-only, which is how the dev setup runs behind Vite.
func (c *Config) StaticDir() string {
	dir := envOr("STATIC_DIR", "./static")
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return ""
	}
	return abs
}

func (c *Config) TmpDir() string   { return filepath.Join(c.DataDir, "tmp") }
func (c *Config) CacheDir() string { return filepath.Join(c.DataDir, "cache") }

// DBPath turns the SQLAlchemy-style URL in .env into a plain file path.
func (c *Config) DBPath() string {
	p := c.DatabaseURL
	for _, prefix := range []string{"sqlite:////", "sqlite:///", "sqlite://"} {
		if len(p) > len(prefix) && p[:len(prefix)] == prefix {
			rest := p[len(prefix):]
			if prefix == "sqlite:////" {
				return "/" + rest
			}
			return rest
		}
	}
	return p
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
