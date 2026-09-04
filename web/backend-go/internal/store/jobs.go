package store

import (
	"database/sql"
	"errors"
	"strings"
)

const jobCols = `id,user_id,profile_id,source_account,dest_account,COALESCE(status,''),pid,logfile,
	started_at,finished_at,exit_code,COALESCE(messages_transferred,0),
	COALESCE(bytes_transferred,0),COALESCE(errors_count,0),summary`

func scanJob(s interface{ Scan(...any) error }) (*SyncJob, error) {
	var j SyncJob
	var started, finished sql.NullTime
	err := s.Scan(&j.ID, &j.UserID, &j.ProfileID, &j.SourceAccount, &j.DestAccount, &j.Status,
		&j.PID, &j.Logfile, &started, &finished, &j.ExitCode,
		&j.MessagesTransferred, &j.BytesTransferred, &j.ErrorsCount, &j.Summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	j.StartedAt = naivePtr(started)
	j.FinishedAt = naivePtr(finished)
	return &j, nil
}

func (db *DB) CreateJob(j *SyncJob) error {
	res, err := db.Exec(`INSERT INTO sync_jobs
		(user_id,profile_id,source_account,dest_account,status,started_at,
		 messages_transferred,bytes_transferred,errors_count)
		VALUES (?,?,?,?,?,?,0,0,0)`,
		j.UserID, j.ProfileID, j.SourceAccount, j.DestAccount, j.Status, naiveArg(j.StartedAt))
	if err != nil {
		return err
	}
	j.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) JobByID(id int64) (*SyncJob, error) {
	return scanJob(db.QueryRow(`SELECT `+jobCols+` FROM sync_jobs WHERE id=?`, id))
}

func (db *DB) JobByIDForUser(id, userID int64) (*SyncJob, error) {
	return scanJob(db.QueryRow(`SELECT `+jobCols+` FROM sync_jobs WHERE id=? AND user_id=?`, id, userID))
}

// JobBelongsTo answers the WebSocket's only question without loading the row.
func (db *DB) JobBelongsTo(jobID, userID int64) bool {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sync_jobs WHERE id=? AND user_id=?`, jobID, userID).Scan(&n)
	return err == nil && n > 0
}

func (db *DB) SetJobPID(id int64, pid int) error {
	_, err := db.Exec(`UPDATE sync_jobs SET pid=? WHERE id=?`, pid, id)
	return err
}

func (db *DB) SetJobLogfile(id int64, path string) error {
	_, err := db.Exec(`UPDATE sync_jobs SET logfile=? WHERE id=?`, path, id)
	return err
}

func (db *DB) RunningJobs(userID int64) ([]*SyncJob, error) {
	return db.queryJobs(`SELECT `+jobCols+` FROM sync_jobs WHERE user_id=? AND status='running' ORDER BY id DESC`, userID)
}

// FinishJob records the outcome. A user-requested abort makes imapsync exit
// non-zero, so an already-aborted job keeps that status instead of being
// relabelled "failed".
func (db *DB) FinishJob(id int64, status string, exitCode int, s Summary) error {
	_, err := db.Exec(`UPDATE sync_jobs SET status=?,exit_code=?,finished_at=?,
		messages_transferred=?,bytes_transferred=?,errors_count=?,summary=? WHERE id=?`,
		status, exitCode, NowVN().Format(TimeLayout),
		s.MessagesTransferred, s.BytesTransferred, s.ErrorsCount, s.Text, id)
	return err
}

// UpdateJobCounts records what a job has moved so far. Until this existed the
// counters were written only by FinishJob, so a migration that ran for hours
// showed 0 messages and 0 bytes in History for its entire life.
//
// The guard matters: it only ever advances a running job's counters, so a
// straggling update cannot resurrect or rewrite a job that has already
// finished, aborted, or been reconciled.
func (db *DB) UpdateJobCounts(id int64, messages int, bytes int64) error {
	_, err := db.Exec(`UPDATE sync_jobs SET messages_transferred=?, bytes_transferred=?
		WHERE id=? AND status='running'`, messages, bytes, id)
	return err
}

func (db *DB) MarkAborted(id int64) error {
	_, err := db.Exec(`UPDATE sync_jobs SET status='aborted', finished_at=? WHERE id=?`,
		NowVN().Format(TimeLayout), id)
	return err
}

// ReconcileOrphans marks jobs left "running" by a crash or restart. The
// registry of live subprocesses is in memory, so a restart loses track of them
// and those rows would stay "running" forever.
func (db *DB) ReconcileOrphans() (int64, error) {
	res, err := db.Exec(`UPDATE sync_jobs
		SET status='interrupted', finished_at=?,
		    summary=COALESCE(summary,'') || char(10) || 'Job interrupted: the server restarted while it was running.'
		WHERE status='running'`, NowVN().Format(TimeLayout))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Summary holds the numbers parsed out of an imapsync run.
type Summary struct {
	MessagesTransferred int
	BytesTransferred    int64
	ErrorsCount         int
	Text                string
	Aborted             bool
}

type HistoryFilter struct {
	UserID  int64
	Status  string
	Query   string
	Page    int
	PerPage int
}

// historyWhere builds the filter shared by the list and its count, so the
// X-Total-Count header describes the query the caller actually made.
func historyWhere(f HistoryFilter) (string, []any) {
	where := []string{"user_id=?"}
	args := []any{f.UserID}
	if f.Status != "" {
		where = append(where, "status=?")
		args = append(args, f.Status)
	}
	if f.Query != "" {
		// Escape the LIKE metacharacters. Without this a search for "_" matched
		// every row and a search for "%" matched everything -- not a security
		// problem (the query is still parameterised and still scoped to the
		// user) but wrong results for anyone whose account name contains an
		// underscore, which is common.
		where = append(where,
			`(COALESCE(summary,'') LIKE ? ESCAPE '\' `+
				`OR COALESCE(source_account,'') LIKE ? ESCAPE '\' `+
				`OR COALESCE(dest_account,'') LIKE ? ESCAPE '\')`)
		like := "%" + escapeLike(f.Query) + "%"
		args = append(args, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func (db *DB) History(f HistoryFilter) ([]*SyncJob, error) {
	where, args := historyWhere(f)
	if f.Page < 1 {
		f.Page = 1
	}
	// Clamp to the ceiling rather than falling back to the default: a caller
	// asking for 200 got 20 rows while X-Total-Count reported the full total,
	// so the client computed the wrong number of pages.
	if f.PerPage < 1 {
		f.PerPage = 20
	}
	if f.PerPage > 100 {
		f.PerPage = 100
	}
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)

	return db.queryJobs(`SELECT `+jobCols+` FROM sync_jobs WHERE `+where+
		` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
}

// HistoryCount counts the same rows History would return, ignoring paging.
func (db *DB) HistoryCount(f HistoryFilter) (int, error) {
	where, args := historyWhere(f)
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sync_jobs WHERE `+where, args...).Scan(&n)
	return n, err
}

type Stats struct {
	TotalSyncs    int     `json:"total_syncs"`
	Successful    int     `json:"successful"`
	Failed        int     `json:"failed"`
	TotalMessages int64   `json:"total_messages"`
	TotalBytes    int64   `json:"total_bytes"`
	SuccessRate   float64 `json:"success_rate"`
}

func (db *DB) Stats(userID int64) (*Stats, error) {
	var s Stats
	// Every SUM needs COALESCE: over zero rows SUM returns NULL, not 0, and a
	// brand-new account has zero rows.
	err := db.QueryRow(`SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(messages_transferred),0),
		COALESCE(SUM(bytes_transferred),0)
		FROM sync_jobs WHERE user_id=?`, userID).
		Scan(&s.TotalSyncs, &s.Successful, &s.Failed, &s.TotalMessages, &s.TotalBytes)
	if err != nil {
		return nil, err
	}
	if s.TotalSyncs > 0 {
		s.SuccessRate = float64(int(float64(s.Successful)/float64(s.TotalSyncs)*1000+0.5)) / 10
	}
	return &s, nil
}

func (db *DB) RecentJobs(userID int64, limit int) ([]*SyncJob, error) {
	return db.queryJobs(`SELECT `+jobCols+` FROM sync_jobs WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
}

func (db *DB) queryJobs(q string, args ...any) ([]*SyncJob, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SyncJob{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// naiveArg renders a nullable timestamp for SQLite in SQLAlchemy's layout.
func naiveArg(t *NaiveTime) any {
	if t == nil {
		return nil
	}
	return t.Format(TimeLayout)
}

// DeleteJob removes one job, returning its log file path so the caller can
// delete that too — the row is small, the log is not.
func (db *DB) DeleteJob(id, userID int64) (string, error) {
	var logfile sql.NullString
	var status string
	err := db.QueryRow(`SELECT COALESCE(status,''), logfile FROM sync_jobs WHERE id=? AND user_id=?`,
		id, userID).Scan(&status, &logfile)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if status == "running" {
		return "", ErrJobRunning
	}
	if _, err := db.Exec(`DELETE FROM sync_jobs WHERE id=? AND user_id=?`, id, userID); err != nil {
		return "", err
	}
	return logfile.String, nil
}

// ErrJobRunning is returned rather than silently deleting the row of a job
// whose process is still writing to it.
var ErrJobRunning = errors.New("job is still running")

// PurgeJobs deletes finished jobs started before the cutoff, returning their
// ids so their logs can be removed too. Running jobs are never touched.
//
// started_at is stored zero-padded, so a plain string comparison orders it
// correctly without parsing.
func (db *DB) PurgeJobs(userID int64, before NaiveTime) ([]int64, error) {
	const where = `user_id=? AND status<>'running' AND started_at IS NOT NULL AND started_at < ?`
	cutoff := before.Format(TimeLayout)

	rows, err := db.Query(`SELECT id FROM sync_jobs WHERE `+where, userID, cutoff)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	if _, err := db.Exec(`DELETE FROM sync_jobs WHERE `+where, userID, cutoff); err != nil {
		return nil, err
	}
	return ids, nil
}

// escapeLike neutralises the wildcards in a user's search term. The backslash
// is declared as the escape character by every LIKE that uses this.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
