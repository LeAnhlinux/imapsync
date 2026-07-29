package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func user(t *testing.T, db *DB, name string) *User {
	t.Helper()
	u := &User{Username: name, Email: name + "@x.com", HashedPassword: "h", IsAdmin: true}
	if err := db.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	for i := 0; i < 3; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		db.Close()
	}
}

func TestNaiveTimeJSON(t *testing.T) {
	// Python's datetime.isoformat() emits no timezone suffix. Go's default
	// marshalling appends "Z", which tells the browser the value is UTC and
	// shifts every displayed timestamp by the viewer's offset.
	tm := NaiveTime{time.Date(2026, 7, 29, 8, 2, 44, 247921000, time.UTC)}
	b, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `"2026-07-29T08:02:44.247921"`; got != want {
		t.Errorf("marshalled %s, want %s", got, want)
	}
	if strings.Contains(string(b), "Z") {
		t.Error("the timestamp carries a timezone suffix")
	}

	b, _ = json.Marshal(NaiveTime{})
	if string(b) != "null" {
		t.Errorf("zero time marshalled to %s, want null", b)
	}
}

func TestNowVNWallClockIsSevenHoursAhead(t *testing.T) {
	// NowVN is the same instant as UTC in a different zone — subtracting the
	// two gives zero. What is stored is the *wall clock reading*, so that is
	// what has to be seven hours ahead, and it must not depend on the host's
	// timezone: the server runs UTC.
	now := time.Now().UTC()
	wall, err := time.Parse(TimeLayout, NowVN().Format(TimeLayout))
	if err != nil {
		t.Fatal(err)
	}
	if diff := wall.Sub(now).Round(time.Minute); diff != 7*time.Hour {
		t.Errorf("stored wall clock is %v ahead of UTC, want 7h", diff)
	}
}

func TestFormatAccount(t *testing.T) {
	// user1 is usually already an address; joining with @ produced
	// "me@old.com@mail.old.com".
	if got := FormatAccount("me@old.com", "mail.old.com"); got != "me@old.com (mail.old.com)" {
		t.Errorf("got %q", got)
	}
	if strings.Count(FormatAccount("me@old.com", "mail.old.com"), "@") != 1 {
		t.Error("the label has more than one @")
	}
}

func TestJSONMapRoundTrip(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	p := &Profile{
		UserID: u.ID, Name: "p", Host1: "h1", User1: "u1", Password1Enc: "e1",
		Host2: "h2", User2: "u2", Password2Enc: "e2",
		ExtraOptions: JSONMap{"automap": true, "maxage": "30"},
	}
	if err := db.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	got, err := db.ProfileByID(p.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExtraOptions["automap"] != true || got.ExtraOptions["maxage"] != "30" {
		t.Errorf("extra_options round-tripped as %v", got.ExtraOptions)
	}
}

func TestProfileOwnership(t *testing.T) {
	db := open(t)
	a, b := user(t, db, "a"), user(t, db, "b")
	p := &Profile{UserID: a.ID, Name: "p", Host1: "h", User1: "u", Password1Enc: "e",
		Host2: "h", User2: "u", Password2Enc: "e"}
	if err := db.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ProfileByID(p.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user could read the profile: %v", err)
	}
	if err := db.DeleteProfile(p.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user could delete the profile: %v", err)
	}
}

func job(t *testing.T, db *DB, userID int64, status string, started time.Time) *SyncJob {
	t.Helper()
	at := NaiveTime{started}
	src, dst := "a@x", "b@y"
	j := &SyncJob{UserID: userID, Status: status, StartedAt: &at, SourceAccount: &src, DestAccount: &dst}
	if err := db.CreateJob(j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestStatsOnEmptyDatabase(t *testing.T) {
	// SUM over zero rows is NULL, not 0 — this 500'd for every new account.
	db := open(t)
	u := user(t, db, "a")
	s, err := db.Stats(u.ID)
	if err != nil {
		t.Fatalf("stats on an empty history failed: %v", err)
	}
	if s.TotalSyncs != 0 || s.TotalMessages != 0 || s.SuccessRate != 0 {
		t.Errorf("got %+v, want zeroes", s)
	}
}

func TestHistoryFilterAndCountAgree(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	now := NowVN()
	job(t, db, u.ID, "completed", now)
	job(t, db, u.ID, "completed", now)
	job(t, db, u.ID, "failed", now)

	for _, c := range []struct {
		status string
		want   int
	}{{"", 3}, {"completed", 2}, {"failed", 1}, {"aborted", 0}} {
		f := HistoryFilter{UserID: u.ID, Status: c.status}
		rows, err := db.History(f)
		if err != nil {
			t.Fatal(err)
		}
		n, err := db.HistoryCount(f)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != c.want || n != c.want {
			t.Errorf("status=%q: %d rows, count %d, want %d", c.status, len(rows), n, c.want)
		}
	}
}

func TestDeleteJobRefusesWhileRunning(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	j := job(t, db, u.ID, "running", NowVN())

	if _, err := db.DeleteJob(j.ID, u.ID); !errors.Is(err, ErrJobRunning) {
		t.Errorf("deleting a running job gave %v, want ErrJobRunning", err)
	}
	if _, err := db.JobByID(j.ID); err != nil {
		t.Error("the job was removed despite the refusal")
	}

	if err := db.FinishJob(j.ID, "completed", 0, Summary{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeleteJob(j.ID, u.ID); err != nil {
		t.Errorf("deleting a finished job failed: %v", err)
	}
}

func TestDeleteJobOwnership(t *testing.T) {
	db := open(t)
	a, b := user(t, db, "a"), user(t, db, "b")
	j := job(t, db, a.ID, "completed", NowVN())
	if _, err := db.DeleteJob(j.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user deleted the job: %v", err)
	}
}

func TestPurgeRespectsAgeAndRunningJobs(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	old := job(t, db, u.ID, "completed", NowVN().AddDate(0, 0, -60))
	recent := job(t, db, u.ID, "completed", NowVN())
	running := job(t, db, u.ID, "running", NowVN().AddDate(0, 0, -60))

	ids, err := db.PurgeJobs(u.ID, NaiveTime{NowVN().AddDate(0, 0, -30)})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != old.ID {
		t.Fatalf("purged %v, want just the 60-day-old job %d", ids, old.ID)
	}
	for _, keep := range []int64{recent.ID, running.ID} {
		if _, err := db.JobByID(keep); err != nil {
			t.Errorf("job %d should have survived the purge", keep)
		}
	}
}

func TestReconcileOrphans(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	j := job(t, db, u.ID, "running", NowVN())

	n, err := db.ReconcileOrphans()
	if err != nil || n != 1 {
		t.Fatalf("reconciled %d err=%v, want 1", n, err)
	}
	got, _ := db.JobByID(j.ID)
	if got.Status != "interrupted" {
		t.Errorf("status is %q, want interrupted", got.Status)
	}
}

func TestRetentionDefaultsToOff(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	r, err := db.RetentionFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Enabled {
		t.Error("retention defaults to on; deleting history unasked is a surprise")
	}
	if r.KeepDays != 30 {
		t.Errorf("default keep_days is %d, want 30", r.KeepDays)
	}

	r.Enabled, r.KeepDays = true, 7
	if err := db.SaveRetention(r); err != nil {
		t.Fatal(err)
	}
	// Saving twice must update rather than collide on the unique user_id.
	if err := db.SaveRetention(r); err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	enabled, err := db.EnabledRetentions()
	if err != nil || len(enabled) != 1 || enabled[0].KeepDays != 7 {
		t.Errorf("enabled retentions = %+v err=%v", enabled, err)
	}
}

func TestAPIKeyScopesAndExpiry(t *testing.T) {
	db := open(t)
	u := user(t, db, "a")
	exp := NaiveTime{NowVN().AddDate(0, 0, 1)}
	k := &APIKey{UserID: u.ID, Name: "k", Prefix: "isk_abc", Scopes: "sync:read", RateLimit: 60, ExpiresAt: &exp}
	if err := db.CreateAPIKey(k, "hash-abc"); err != nil {
		t.Fatal(err)
	}

	got, err := db.APIKeyByHash("hash-abc")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Allows("sync:read") || got.Allows("admin") || !got.Usable() {
		t.Errorf("key reads as %+v", got)
	}
	if _, err := db.APIKeyByHash("no-such-hash"); !errors.Is(err, ErrNotFound) {
		t.Error("an unknown hash resolved to a key")
	}

	if err := db.RevokeAPIKey(k.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = db.APIKeyByHash("hash-abc")
	if got.Usable() {
		t.Error("a revoked key is still usable")
	}
}

func TestWebhookSubscription(t *testing.T) {
	h := &Webhook{Events: "job.completed,job.failed"}
	if !h.Subscribes("job.completed") || h.Subscribes("job.started") {
		t.Error("event matching is wrong")
	}
	if !(&Webhook{Events: "*"}).Subscribes("anything") {
		t.Error("* should match every event")
	}
}
