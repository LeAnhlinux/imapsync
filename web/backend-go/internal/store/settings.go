package store

import (
	"database/sql"
	"errors"
)

// ---------------------------------------------------------------- schedules

const scheduleCols = `id,user_id,profile_id,cron_expression,COALESCE(is_active,1),
	last_run_at,next_run_at,created_at`

func scanSchedule(s interface{ Scan(...any) error }) (*Schedule, error) {
	var sc Schedule
	var last, next, created sql.NullTime
	err := s.Scan(&sc.ID, &sc.UserID, &sc.ProfileID, &sc.CronExpression, &sc.IsActive,
		&last, &next, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sc.LastRunAt = naivePtr(last)
	sc.NextRunAt = naivePtr(next)
	sc.CreatedAt = naive(created)
	return &sc, nil
}

func (db *DB) SchedulesByUser(userID int64) ([]*Schedule, error) {
	return db.querySchedules(`SELECT `+scheduleCols+` FROM schedules WHERE user_id=? ORDER BY id`, userID)
}

func (db *DB) ActiveSchedules() ([]*Schedule, error) {
	return db.querySchedules(`SELECT ` + scheduleCols + ` FROM schedules WHERE is_active=1`)
}

func (db *DB) ScheduleByID(id, userID int64) (*Schedule, error) {
	return scanSchedule(db.QueryRow(`SELECT `+scheduleCols+` FROM schedules WHERE id=? AND user_id=?`, id, userID))
}

func (db *DB) ScheduleByIDAny(id int64) (*Schedule, error) {
	return scanSchedule(db.QueryRow(`SELECT `+scheduleCols+` FROM schedules WHERE id=?`, id))
}

func (db *DB) CreateSchedule(s *Schedule) error {
	res, err := db.Exec(`INSERT INTO schedules (user_id,profile_id,cron_expression,is_active,created_at)
		VALUES (?,?,?,?,?)`, s.UserID, s.ProfileID, s.CronExpression, s.IsActive, NowVN().Format(TimeLayout))
	if err != nil {
		return err
	}
	s.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) UpdateSchedule(s *Schedule) error {
	_, err := db.Exec(`UPDATE schedules SET cron_expression=?, is_active=? WHERE id=? AND user_id=?`,
		s.CronExpression, s.IsActive, s.ID, s.UserID)
	return err
}

func (db *DB) DeleteSchedule(id, userID int64) error {
	res, err := db.Exec(`DELETE FROM schedules WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) TouchScheduleRun(id int64, next *NaiveTime) error {
	_, err := db.Exec(`UPDATE schedules SET last_run_at=?, next_run_at=? WHERE id=?`,
		NowVN().Format(TimeLayout), naiveArg(next), id)
	return err
}

func (db *DB) querySchedules(q string, args ...any) ([]*Schedule, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- batches

const batchCols = `id,user_id,filename,COALESCE(total_rows,0),COALESCE(completed_rows,0),
	COALESCE(failed_rows,0),COALESCE(status,''),batch_data,created_at`

func scanBatch(s interface{ Scan(...any) error }) (*BatchRun, error) {
	var b BatchRun
	var created sql.NullTime
	err := s.Scan(&b.ID, &b.UserID, &b.Filename, &b.TotalRows, &b.CompletedRows,
		&b.FailedRows, &b.Status, &b.BatchData, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	b.CreatedAt = naive(created)
	return &b, nil
}

func (db *DB) CreateBatch(b *BatchRun) error {
	res, err := db.Exec(`INSERT INTO batch_runs
		(user_id,filename,total_rows,completed_rows,failed_rows,status,batch_data,created_at)
		VALUES (?,?,?,0,0,?,?,?)`,
		b.UserID, b.Filename, b.TotalRows, b.Status, b.BatchData, NowVN().Format(TimeLayout))
	if err != nil {
		return err
	}
	b.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) BatchByID(id, userID int64) (*BatchRun, error) {
	return scanBatch(db.QueryRow(`SELECT `+batchCols+` FROM batch_runs WHERE id=? AND user_id=?`, id, userID))
}

func (db *DB) SetBatchStatus(id int64, status string) error {
	_, err := db.Exec(`UPDATE batch_runs SET status=? WHERE id=?`, status, id)
	return err
}

func (db *DB) BumpBatchCounts(id int64, completed, failed int) error {
	_, err := db.Exec(`UPDATE batch_runs SET completed_rows=completed_rows+?, failed_rows=failed_rows+? WHERE id=?`,
		completed, failed, id)
	return err
}

// FinishBatch clears batch_data along with the status: it holds credentials.
func (db *DB) FinishBatch(id int64) error {
	_, err := db.Exec(`UPDATE batch_runs SET status='completed', batch_data=NULL WHERE id=?`, id)
	return err
}

// ---------------------------------------------------- notification settings

func (db *DB) NotificationSettings(userID int64) (*NotificationSettings, error) {
	var n NotificationSettings
	err := db.QueryRow(`SELECT id,user_id,smtp_host,COALESCE(smtp_port,587),smtp_user,smtp_password_enc,
		from_email,to_email,COALESCE(notify_on_success,0),COALESCE(notify_on_failure,1)
		FROM notification_settings WHERE user_id=?`, userID).
		Scan(&n.ID, &n.UserID, &n.SMTPHost, &n.SMTPPort, &n.SMTPUser, &n.SMTPPasswordEnc,
			&n.FromEmail, &n.ToEmail, &n.NotifyOnSuccess, &n.NotifyOnFailure)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (db *DB) SaveNotificationSettings(n *NotificationSettings) error {
	existing, err := db.NotificationSettings(n.UserID)
	if errors.Is(err, ErrNotFound) {
		_, err := db.Exec(`INSERT INTO notification_settings
			(user_id,smtp_host,smtp_port,smtp_user,smtp_password_enc,from_email,to_email,notify_on_success,notify_on_failure)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			n.UserID, n.SMTPHost, n.SMTPPort, n.SMTPUser, n.SMTPPasswordEnc,
			n.FromEmail, n.ToEmail, n.NotifyOnSuccess, n.NotifyOnFailure)
		return err
	}
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE notification_settings SET smtp_host=?,smtp_port=?,smtp_user=?,
		smtp_password_enc=?,from_email=?,to_email=?,notify_on_success=?,notify_on_failure=? WHERE id=?`,
		n.SMTPHost, n.SMTPPort, n.SMTPUser, n.SMTPPasswordEnc,
		n.FromEmail, n.ToEmail, n.NotifyOnSuccess, n.NotifyOnFailure, existing.ID)
	return err
}

// ------------------------------------------------------- telegram settings

func (db *DB) TelegramSettings() (*TelegramSettings, error) {
	var t TelegramSettings
	err := db.QueryRow(`SELECT id,COALESCE(enabled,0),bot_token_enc,COALESCE(allowed_chat_ids,''),user_id,
		COALESCE(notify_on_success,1),COALESCE(notify_on_failure,1),
		COALESCE(notify_on_progress,1),COALESCE(progress_step,25)
		FROM telegram_settings WHERE id=1`).
		Scan(&t.ID, &t.Enabled, &t.BotTokenEnc, &t.AllowedChatIDs, &t.UserID,
			&t.NotifyOnSuccess, &t.NotifyOnFailure, &t.NotifyOnProgress, &t.ProgressStep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TelegramSettingsOrCreate returns the single settings row, creating a
// disabled one the first time the settings page is opened.
func (db *DB) TelegramSettingsOrCreate() (*TelegramSettings, error) {
	t, err := db.TelegramSettings()
	if errors.Is(err, ErrNotFound) {
		if _, err := db.Exec(`INSERT INTO telegram_settings
			(id,enabled,notify_on_success,notify_on_failure,notify_on_progress,progress_step,updated_at)
			VALUES (1,0,1,1,1,25,?)`, NowVN().Format(TimeLayout)); err != nil {
			return nil, err
		}
		return db.TelegramSettings()
	}
	return t, err
}

func (db *DB) SaveTelegramSettings(t *TelegramSettings) error {
	_, err := db.Exec(`UPDATE telegram_settings SET enabled=?,bot_token_enc=?,allowed_chat_ids=?,
		user_id=?,notify_on_success=?,notify_on_failure=?,notify_on_progress=?,progress_step=?,updated_at=?
		WHERE id=1`,
		t.Enabled, t.BotTokenEnc, t.AllowedChatIDs, t.UserID,
		t.NotifyOnSuccess, t.NotifyOnFailure, t.NotifyOnProgress, t.ProgressStep,
		NowVN().Format(TimeLayout))
	return err
}

// ------------------------------------------------------- retention settings

// Retention describes how long one account keeps finished jobs before the
// nightly sweeper removes them and their logs.
type Retention struct {
	UserID      int64      `json:"-"`
	Enabled     bool       `json:"enabled"`
	KeepDays    int        `json:"keep_days"`
	LastRunAt   *NaiveTime `json:"last_run_at"`
	LastDeleted int        `json:"last_deleted"`
}

const retentionCols = `user_id,COALESCE(enabled,0),COALESCE(keep_days,30),last_run_at,COALESCE(last_deleted,0)`

func scanRetention(s interface{ Scan(...any) error }) (*Retention, error) {
	var r Retention
	var last sql.NullTime
	if err := s.Scan(&r.UserID, &r.Enabled, &r.KeepDays, &last, &r.LastDeleted); err != nil {
		return nil, err
	}
	r.LastRunAt = naivePtr(last)
	return &r, nil
}

// RetentionFor returns the account's settings, defaulting to disabled: a tool
// that starts silently deleting history would be a surprise.
func (db *DB) RetentionFor(userID int64) (*Retention, error) {
	r, err := scanRetention(db.QueryRow(
		`SELECT `+retentionCols+` FROM retention_settings WHERE user_id=?`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return &Retention{UserID: userID, Enabled: false, KeepDays: 30}, nil
	}
	return r, err
}

func (db *DB) SaveRetention(r *Retention) error {
	_, err := db.Exec(`INSERT INTO retention_settings (user_id,enabled,keep_days)
		VALUES (?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled, keep_days=excluded.keep_days`,
		r.UserID, r.Enabled, r.KeepDays)
	return err
}

func (db *DB) RecordRetentionRun(userID int64, deleted int) error {
	_, err := db.Exec(`UPDATE retention_settings SET last_run_at=?, last_deleted=? WHERE user_id=?`,
		NowVN().Format(TimeLayout), deleted, userID)
	return err
}

// EnabledRetentions is what the nightly sweeper iterates.
func (db *DB) EnabledRetentions() ([]*Retention, error) {
	rows, err := db.Query(`SELECT ` + retentionCols +
		` FROM retention_settings WHERE enabled=1 AND COALESCE(keep_days,0) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Retention
	for rows.Next() {
		r, err := scanRetention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AbortBatch records a cancelled run and clears its credentials, the same way
// finishing does.
func (db *DB) AbortBatch(id int64) error {
	_, err := db.Exec(`UPDATE batch_runs SET status='aborted', batch_data=NULL WHERE id=?`, id)
	return err
}

// ReconcileOrphanBatches marks batches left running by a crash or restart.
//
// Which batches are live is in-memory state, so a restart loses it: the row
// stayed 'running' forever, the API and the bot both refused to start it again
// ("already running"), Cancel answered "not running" because the map was empty,
// and batch_data — holding the encrypted credentials of every mailbox not yet
// reached — was never cleared. ReconcileOrphans does the same for sync_jobs.
func (db *DB) ReconcileOrphanBatches() (int64, error) {
	res, err := db.Exec(
		`UPDATE batch_runs SET status='interrupted', batch_data=NULL WHERE status='running'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
