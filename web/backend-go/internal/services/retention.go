package services

import (
	"fmt"
	"log"
	"sync"

	"github.com/robfig/cron/v3"

	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/store"
)

// Retention sweeps old finished jobs out of the history once a day.
//
// It exists because the job row is not the expensive part: each one keeps an
// imapsync log beside it, and without a sweeper those accumulate until someone
// notices the disk.
type Retention struct {
	DB     *store.DB
	Runner *runner.Runner

	mu   sync.Mutex
	cron *cron.Cron
}

// Sweep hour is early morning local time, when a migration is least likely to
// be running and competing for the same database.
const sweepSchedule = "30 3 * * *"

func (rt *Retention) Start() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cron != nil {
		rt.cron.Stop()
	}
	// Same zone as everything else. The 03:30 slot below is only "when a
	// migration is least likely to be running" if it means 03:30 locally.
	rt.cron = cron.New(cron.WithLocation(store.VN))
	if _, err := rt.cron.AddFunc(sweepSchedule, rt.Sweep); err != nil {
		log.Printf("retention: could not schedule the sweep: %v", err)
		return
	}
	rt.cron.Start()
}

func (rt *Retention) Stop() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cron != nil {
		rt.cron.Stop()
	}
}

// Sweep purges every account that has retention switched on. Exported so the
// API can trigger it on demand — "run it now" is the only way to find out the
// setting does what you expect without waiting a day.
func (rt *Retention) Sweep() {
	settings, err := rt.DB.EnabledRetentions()
	if err != nil {
		log.Printf("retention: %v", err)
		return
	}
	for _, s := range settings {
		n, err := rt.SweepUser(s.UserID, s.KeepDays)
		if err != nil {
			log.Printf("retention: user %d: %v", s.UserID, err)
			continue
		}
		if n > 0 {
			log.Printf("retention: removed %d job(s) older than %d days for user %d",
				n, s.KeepDays, s.UserID)
		}
	}
}

// SweepUser purges one account and returns how many jobs went.
func (rt *Retention) SweepUser(userID int64, keepDays int) (int, error) {
	// The API and the EnabledRetentions query both guard this, but "run now"
	// reaches here with whatever is in the row: a 0 written directly to the
	// database would delete every finished job in the history.
	if keepDays < 1 {
		return 0, fmt.Errorf("keep_days must be at least 1, got %d", keepDays)
	}

	cutoff := store.NaiveTime{Time: store.NowVN().AddDate(0, 0, -keepDays)}
	ids, err := rt.DB.PurgeJobs(userID, cutoff)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		rt.Runner.DeleteLog(id)
	}
	if err := rt.DB.RecordRetentionRun(userID, len(ids)); err != nil {
		// The purge already happened; failing to record it is not worth
		// reporting as a failed sweep.
		log.Printf("retention: could not record the run for user %d: %v", userID, err)
	}
	return len(ids), nil
}
