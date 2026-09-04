package store

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotFound is returned instead of sql.ErrNoRows so callers do not have to
// import database/sql to tell "missing" from "broken".
var ErrNotFound = errors.New("not found")

// JSONMap stores a map in a SQLAlchemy JSON column.
type JSONMap map[string]any

func (j *JSONMap) Scan(src any) error {
	if src == nil {
		*j = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONMap", src)
	}
	if len(raw) == 0 {
		*j = nil
		return nil
	}
	return json.Unmarshal(raw, j)
}

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

type User struct {
	ID             int64     `json:"id"`
	Username       string    `json:"username"`
	Email          string    `json:"email"`
	HashedPassword string    `json:"-"`
	IsAdmin        bool      `json:"is_admin"`
	CreatedAt      NaiveTime `json:"created_at"`
}

type Profile struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"-"`
	Name         string    `json:"name"`
	Host1        string    `json:"host1"`
	User1        string    `json:"user1"`
	Password1Enc string    `json:"-"`
	SSL1         bool      `json:"ssl1"`
	Host2        string    `json:"host2"`
	User2        string    `json:"user2"`
	Password2Enc string    `json:"-"`
	SSL2         bool      `json:"ssl2"`
	ExtraOptions JSONMap   `json:"extra_options"`
	CreatedAt    NaiveTime `json:"created_at"`
	UpdatedAt    NaiveTime `json:"updated_at"`
}

type SyncJob struct {
	ID                  int64      `json:"id"`
	UserID              int64      `json:"user_id"`
	ProfileID           *int64     `json:"profile_id"`
	SourceAccount       *string    `json:"source_account"`
	DestAccount         *string    `json:"dest_account"`
	Status              string     `json:"status"`
	PID                 *int       `json:"pid"`
	Logfile             *string    `json:"-"`
	StartedAt           *NaiveTime `json:"started_at"`
	FinishedAt          *NaiveTime `json:"finished_at"`
	ExitCode            *int       `json:"exit_code"`
	MessagesTransferred int        `json:"messages_transferred"`
	BytesTransferred    int64      `json:"bytes_transferred"`
	ErrorsCount         int        `json:"errors_count"`
	Summary             *string    `json:"summary"`
}

type Schedule struct {
	ID             int64      `json:"id"`
	UserID         int64      `json:"-"`
	ProfileID      int64      `json:"profile_id"`
	CronExpression string     `json:"cron_expression"`
	IsActive       bool       `json:"is_active"`
	LastRunAt      *NaiveTime `json:"last_run_at"`
	NextRunAt      *NaiveTime `json:"next_run_at"`
	CreatedAt      NaiveTime  `json:"created_at"`
}

type BatchRun struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"-"`
	Filename      string    `json:"filename"`
	TotalRows     int       `json:"total_rows"`
	CompletedRows int       `json:"completed_rows"`
	FailedRows    int       `json:"failed_rows"`
	Status        string    `json:"status"`
	BatchData     JSONMap   `json:"-"`
	CreatedAt     NaiveTime `json:"created_at"`
}

type NotificationSettings struct {
	ID              int64   `json:"-"`
	UserID          int64   `json:"-"`
	SMTPHost        *string `json:"smtp_host"`
	SMTPPort        int     `json:"smtp_port"`
	SMTPUser        *string `json:"smtp_user"`
	SMTPPasswordEnc *string `json:"-"`
	FromEmail       *string `json:"from_email"`
	ToEmail         *string `json:"to_email"`
	NotifyOnSuccess bool    `json:"notify_on_success"`
	NotifyOnFailure bool    `json:"notify_on_failure"`
}

type TelegramSettings struct {
	ID               int64
	Enabled          bool
	BotTokenEnc      *string
	AllowedChatIDs   string
	UserID           *int64
	NotifyOnSuccess  bool
	NotifyOnFailure  bool
	NotifyOnProgress bool
	ProgressStep     int
}

// ---------------------------------------------------------------- users

func (db *DB) CreateUser(u *User) error {
	res, err := db.Exec(
		`INSERT INTO users (username,email,hashed_password,is_admin,created_at) VALUES (?,?,?,?,?)`,
		u.Username, u.Email, u.HashedPassword, u.IsAdmin, NowVN().Format(TimeLayout))
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) UserByUsername(name string) (*User, error) {
	return db.scanUser(db.QueryRow(
		`SELECT id,username,email,hashed_password,COALESCE(is_admin,0),created_at FROM users WHERE username=?`, name))
}

func (db *DB) UserByID(id int64) (*User, error) {
	return db.scanUser(db.QueryRow(
		`SELECT id,username,email,hashed_password,COALESCE(is_admin,0),created_at FROM users WHERE id=?`, id))
}

func (db *DB) scanUser(row *sql.Row) (*User, error) {
	var u User
	var created sql.NullTime
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.HashedPassword, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = naive(created)
	return &u, nil
}

func (db *DB) SetUserPassword(id int64, hash string, admin bool) error {
	_, err := db.Exec(`UPDATE users SET hashed_password=?, is_admin=? WHERE id=?`, hash, admin, id)
	return err
}

// ---------------------------------------------------------------- profiles

const profileCols = `id,user_id,name,host1,user1,password1_enc,COALESCE(ssl1,1),
	host2,user2,password2_enc,COALESCE(ssl2,1),extra_options,created_at,updated_at`

func scanProfile(s interface{ Scan(...any) error }) (*Profile, error) {
	var p Profile
	var created, updated sql.NullTime
	err := s.Scan(&p.ID, &p.UserID, &p.Name, &p.Host1, &p.User1, &p.Password1Enc, &p.SSL1,
		&p.Host2, &p.User2, &p.Password2Enc, &p.SSL2, &p.ExtraOptions, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = naive(created), naive(updated)
	if p.ExtraOptions == nil {
		p.ExtraOptions = JSONMap{}
	}
	return &p, nil
}

func (db *DB) ProfilesByUser(userID int64) ([]*Profile, error) {
	rows, err := db.Query(`SELECT `+profileCols+` FROM profiles WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) ProfileByID(id, userID int64) (*Profile, error) {
	return scanProfile(db.QueryRow(`SELECT `+profileCols+` FROM profiles WHERE id=? AND user_id=?`, id, userID))
}

func (db *DB) CreateProfile(p *Profile) error {
	now := NowVN().Format(TimeLayout)
	res, err := db.Exec(`INSERT INTO profiles
		(user_id,name,host1,user1,password1_enc,ssl1,host2,user2,password2_enc,ssl2,extra_options,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.UserID, p.Name, p.Host1, p.User1, p.Password1Enc, p.SSL1,
		p.Host2, p.User2, p.Password2Enc, p.SSL2, p.ExtraOptions, now, now)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) UpdateProfile(p *Profile) error {
	_, err := db.Exec(`UPDATE profiles SET name=?,host1=?,user1=?,password1_enc=?,ssl1=?,
		host2=?,user2=?,password2_enc=?,ssl2=?,extra_options=?,updated_at=? WHERE id=? AND user_id=?`,
		p.Name, p.Host1, p.User1, p.Password1Enc, p.SSL1,
		p.Host2, p.User2, p.Password2Enc, p.SSL2, p.ExtraOptions,
		NowVN().Format(TimeLayout), p.ID, p.UserID)
	return err
}

// DeleteProfile removes a profile and everything that points at it.
//
// sync_jobs.profile_id and schedules.profile_id are plain REFERENCES with no ON
// DELETE action, and this port turns foreign keys on where the Python one left
// them off. So a bare DELETE failed with "FOREIGN KEY constraint failed" for
// any profile that had ever been used -- which is every profile a user would
// want to delete, and the one case where they want the stored IMAP password
// gone. History is kept: jobs lose the link, not the row, which is what the
// confirmation dialog promises. Schedules cannot outlive the profile they run,
// so they go with it.
func (db *DB) DeleteProfile(id, userID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE sync_jobs SET profile_id=NULL WHERE profile_id=? AND user_id=?`,
		id, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM schedules WHERE profile_id=? AND user_id=?`, id, userID); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM profiles WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
