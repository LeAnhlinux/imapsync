// Package store owns the SQLite database: schema, migrations and queries.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLAlchemy renders DATETIME columns as a naive, space-separated string with
// microseconds. Anything else here silently rewrites every timestamp.
const TimeLayout = "2006-01-02 15:04:05.000000"

// VN is the zone timestamps are written in, matching the Python now_vn().
// The value stored is naive: the offset is applied, then the zone dropped.
var VN = time.FixedZone("UTC+7", 7*3600)

// NowVN returns the current wall-clock time in UTC+7.
func NowVN() time.Time { return time.Now().In(VN) }

// FormatAccount labels a mailbox for display. An IMAP username is usually
// already a full address, so joining it to the host with an @ produced
// "me@old.com@mail.old.com"; and when it is not an address, "u1@a.com" reads
// like one.
func FormatAccount(user, host string) string {
	return fmt.Sprintf("%s (%s)", user, host)
}

type DB struct{ *sql.DB }

// Open connects and prepares the database for concurrent use.
func Open(path string) (*DB, error) {
	// WAL matters even for one process: the runner writes job rows from
	// background goroutines while HTTP handlers read them, and the default
	// rollback journal turns that into "database is locked".
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

// Column types are spelled the way SQLAlchemy spells them, so a database
// created by either backend is readable by the other.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		username VARCHAR(50) NOT NULL,
		email VARCHAR(255) NOT NULL,
		hashed_password VARCHAR(255) NOT NULL,
		is_admin BOOLEAN,
		created_at DATETIME
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ix_users_username ON users (username)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ix_users_email ON users (email)`,

	`CREATE TABLE IF NOT EXISTS profiles (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		name VARCHAR(100) NOT NULL,
		host1 VARCHAR(255) NOT NULL,
		user1 VARCHAR(255) NOT NULL,
		password1_enc TEXT NOT NULL,
		ssl1 BOOLEAN,
		host2 VARCHAR(255) NOT NULL,
		user2 VARCHAR(255) NOT NULL,
		password2_enc TEXT NOT NULL,
		ssl2 BOOLEAN,
		extra_options JSON,
		created_at DATETIME,
		updated_at DATETIME
	)`,

	`CREATE TABLE IF NOT EXISTS sync_jobs (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		profile_id INTEGER REFERENCES profiles(id),
		source_account VARCHAR(255),
		dest_account VARCHAR(255),
		status VARCHAR(20),
		pid INTEGER,
		logfile VARCHAR(500),
		started_at DATETIME,
		finished_at DATETIME,
		exit_code INTEGER,
		messages_transferred INTEGER,
		bytes_transferred INTEGER,
		errors_count INTEGER,
		summary TEXT,
		sync_params JSON
	)`,
	`CREATE INDEX IF NOT EXISTS ix_sync_jobs_status ON sync_jobs (status)`,
	// Every history page, every X-Total-Count, the dashboard stats and the
	// retention sweep all filter on user_id, and only ix_sync_jobs_status
	// existed -- a low-cardinality index that helps none of them. They were
	// full table scans of the one table that grows without bound, since
	// retention defaults to off. The composite also serves the ORDER BY id DESC.
	`CREATE INDEX IF NOT EXISTS ix_sync_jobs_user_id ON sync_jobs (user_id, id DESC)`,

	`CREATE TABLE IF NOT EXISTS schedules (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		profile_id INTEGER NOT NULL REFERENCES profiles(id),
		cron_expression VARCHAR(100) NOT NULL,
		is_active BOOLEAN,
		last_run_at DATETIME,
		next_run_at DATETIME,
		created_at DATETIME
	)`,

	`CREATE TABLE IF NOT EXISTS batch_runs (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		filename VARCHAR(255) NOT NULL,
		total_rows INTEGER,
		completed_rows INTEGER,
		failed_rows INTEGER,
		status VARCHAR(20),
		batch_data JSON,
		created_at DATETIME
	)`,

	`CREATE TABLE IF NOT EXISTS notification_settings (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL UNIQUE REFERENCES users(id),
		smtp_host VARCHAR(255),
		smtp_port INTEGER,
		smtp_user VARCHAR(255),
		smtp_password_enc TEXT,
		from_email VARCHAR(255),
		to_email VARCHAR(255),
		notify_on_success BOOLEAN,
		notify_on_failure BOOLEAN
	)`,

	// Only a SHA-256 digest of an API key is stored; the key itself is shown
	// once, at creation, and never again.
	`CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		name VARCHAR(100) NOT NULL,
		prefix VARCHAR(16) NOT NULL,
		key_hash VARCHAR(64) NOT NULL,
		scopes VARCHAR(255),
		rate_limit_per_minute INTEGER,
		expires_at DATETIME,
		last_used_at DATETIME,
		revoked BOOLEAN,
		created_at DATETIME
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ix_api_keys_hash ON api_keys (key_hash)`,

	// Per user, because jobs are: the sweeper purges each account's own
	// history rather than applying one global cutoff to everybody.
	`CREATE TABLE IF NOT EXISTS retention_settings (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL UNIQUE REFERENCES users(id),
		enabled BOOLEAN,
		keep_days INTEGER,
		last_run_at DATETIME,
		last_deleted INTEGER
	)`,

	`CREATE TABLE IF NOT EXISTS webhooks (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		url VARCHAR(500) NOT NULL,
		secret_enc TEXT,
		events VARCHAR(255),
		is_active BOOLEAN,
		last_status INTEGER,
		last_called_at DATETIME,
		created_at DATETIME
	)`,

	`CREATE TABLE IF NOT EXISTS telegram_settings (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		enabled BOOLEAN,
		bot_token_enc TEXT,
		allowed_chat_ids VARCHAR(500),
		user_id INTEGER REFERENCES users(id),
		notify_on_success BOOLEAN,
		notify_on_failure BOOLEAN,
		notify_on_progress BOOLEAN,
		progress_step INTEGER,
		updated_at DATETIME
	)`,
}

// Columns a database created by an older version may be missing. CREATE TABLE
// IF NOT EXISTS leaves an existing table alone, so added columns need this.
var addedColumns = []struct{ table, column, ddl string }{
	{"sync_jobs", "source_account", "VARCHAR(255)"},
	{"sync_jobs", "dest_account", "VARCHAR(255)"},
	{"sync_jobs", "sync_params", "JSON"},
	{"batch_runs", "batch_data", "JSON"},
	{"telegram_settings", "notify_on_progress", "BOOLEAN"},
	{"telegram_settings", "progress_step", "INTEGER"},
}

func (db *DB) migrate() error {
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	for _, c := range addedColumns {
		has, err := db.hasColumn(c.table, c.column)
		if err != nil {
			return err
		}
		if !has {
			// Every added column is nullable; SQLite refuses NOT NULL here.
			stmt := fmt.Sprintf("ALTER TABLE %q ADD COLUMN %q %s", c.table, c.column, c.ddl)
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", c.table, c.column, err)
			}
		}
	}
	return nil
}

func (db *DB) hasColumn(table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
