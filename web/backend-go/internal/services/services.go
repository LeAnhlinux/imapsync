// Package services wires the store and the runner together: starting syncs,
// recording their outcome, and running them on a schedule.
package services

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strconv"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/imapsync/web-backend/internal/api"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

// Starter creates the SyncJob row and spawns imapsync for it. Every caller —
// the HTTP API, the scheduler, the batch runner, the Telegram bot — goes
// through here so the sequence exists once.
type Starter struct {
	DB     *store.DB
	Sec    *security.Manager
	Runner *runner.Runner
}

func (s *Starter) launch(userID int64, profileID *int64, p runner.Params) (int64, error) {
	now := store.NaiveTime{Time: store.NowVN()}
	src := store.FormatAccount(p.User1, p.Host1)
	dst := store.FormatAccount(p.User2, p.Host2)

	job := &store.SyncJob{
		UserID: userID, ProfileID: profileID,
		SourceAccount: &src, DestAccount: &dst,
		Status: "running", StartedAt: &now,
	}
	if err := s.DB.CreateJob(job); err != nil {
		return 0, err
	}
	_ = s.DB.SetJobLogfile(job.ID, fmt.Sprintf("data/logs/%d.txt", job.ID))

	live, err := s.Runner.Start(job.ID, p)
	if err != nil {
		_ = s.DB.FinishJob(job.ID, "failed", 1, store.Summary{Text: err.Error()})
		return job.ID, err
	}
	_ = s.DB.SetJobPID(job.ID, live.PID())
	return job.ID, nil
}

// StartProfile runs a stored profile. override, when non-nil, is merged over
// the profile's saved options for this run only — the New Sync screen offers
// those checkboxes and then showed the user's choice back to them on the review
// screen, while the run silently used whatever the profile had saved. Unticking
// "use cache" and getting a cached run anyway is the kind of quiet disagreement
// that costs someone a migration.
func (s *Starter) StartProfile(userID, profileID int64, folders []string, dryRun bool,
	override map[string]any) (int64, error) {
	p, err := s.DB.ProfileByID(profileID, userID)
	if err != nil {
		return 0, err
	}
	pw1, err := s.Sec.Decrypt(p.Password1Enc)
	if err != nil {
		return 0, err
	}
	pw2, err := s.Sec.Decrypt(p.Password2Enc)
	if err != nil {
		return 0, err
	}
	opts := map[string]any{}
	for k, v := range p.ExtraOptions {
		opts[k] = v
	}
	for k, v := range override {
		opts[k] = v
	}

	return s.launch(userID, &profileID, runner.Params{
		Host1: p.Host1, User1: p.User1, Password1: pw1,
		Host2: p.Host2, User2: p.User2, Password2: pw2,
		SSL1: p.SSL1, SSL2: p.SSL2,
		ExtraOptions: opts, Folders: folders, DryRun: dryRun,
	})
}

func (s *Starter) StartAdhoc(userID int64, a api.AdhocParams) (int64, error) {
	return s.launch(userID, nil, runner.Params{
		Host1: a.Host1, User1: a.User1, Password1: a.Password1,
		Host2: a.Host2, User2: a.User2, Password2: a.Password2,
		SSL1: a.SSL1, SSL2: a.SSL2,
		ExtraOptions: a.ExtraOptions, Folders: a.Folders, DryRun: a.DryRun,
	})
}

// Completion records a finished job and fans the result out to notifiers.
type Completion struct {
	DB     *store.DB
	Sec    *security.Manager
	Mailer *Mailer
	// Notify is set by the Telegram bot when it is running.
	Notify func(jobID int64, status string, summary string, userID int64)
	// Hooks delivers the same outcome to any registered webhook.
	Hooks *Webhooks
}

func (c *Completion) Finish(jobID int64, exitCode int, s store.Summary) {
	job, err := c.DB.JobByID(jobID)
	if err != nil {
		log.Printf("finish job %d: %v", jobID, err)
		return
	}

	// A user-requested SIGTERM makes imapsync exit non-zero; that is not a
	// failure and must not overwrite the status set by /abort.
	status := "completed"
	if s.Aborted || job.Status == "aborted" {
		status = "aborted"
	} else if exitCode != 0 {
		status = "failed"
	}
	if err := c.DB.FinishJob(jobID, status, exitCode, s); err != nil {
		log.Printf("finish job %d: %v", jobID, err)
	}

	if status != "aborted" {
		// On its own goroutine: even with a deadline, a slow SMTP server should
		// not delay the Telegram message and the webhooks below it.
		go c.Mailer.NotifyJob(job.UserID, jobID, status, s.Text)
	}
	if c.Notify != nil {
		c.Notify(jobID, status, s.Text, job.UserID)
	}
	if c.Hooks != nil {
		c.Hooks.Dispatch("job."+status, job.UserID, jobID)
	}
}

// Mailer sends the SMTP notifications configured per user.
type Mailer struct {
	DB  *store.DB
	Sec *security.Manager
}

func (m *Mailer) SendTest(n *store.NotificationSettings, password string) error {
	return m.send(n, password, "imapsync Web UI - Test Notification",
		"This is a test notification from imapsync Web UI. Your SMTP settings are working correctly.")
}

func (m *Mailer) NotifyJob(userID, jobID int64, status, summary string) {
	n, err := m.DB.NotificationSettings(userID)
	if err != nil || n.SMTPHost == nil || *n.SMTPHost == "" || n.ToEmail == nil {
		return
	}
	if status == "completed" && !n.NotifyOnSuccess {
		return
	}
	if status == "failed" && !n.NotifyOnFailure {
		return
	}

	password := ""
	if n.SMTPPasswordEnc != nil {
		if password, err = m.Sec.Decrypt(*n.SMTPPasswordEnc); err != nil {
			log.Printf("notification for job %d: %v", jobID, err)
			return
		}
	}
	verb := "Completed"
	if status != "completed" {
		verb = "Failed"
	}
	subject := fmt.Sprintf("imapsync Job #%d - %s", jobID, verb)
	body := fmt.Sprintf("Sync Job #%d has %s.\n\nSummary:\n%s", jobID, status, summary)
	if err := m.send(n, password, subject, body); err != nil {
		log.Printf("notification for job %d: %v", jobID, err)
	}
}

func (m *Mailer) send(n *store.NotificationSettings, password, subject, body string) error {
	if n.SMTPHost == nil || n.ToEmail == nil {
		return fmt.Errorf("SMTP not configured")
	}
	from := ""
	if n.FromEmail != nil {
		from = *n.FromEmail
	}
	if from == "" && n.SMTPUser != nil {
		from = *n.SMTPUser
	}

	// JoinHostPort, not Sprintf: an IPv6 SMTP host needs brackets.
	addr := net.JoinHostPort(*n.SMTPHost, strconv.Itoa(n.SMTPPort))
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		from, *n.ToEmail, subject, body)

	var auth smtp.Auth
	if n.SMTPUser != nil && *n.SMTPUser != "" {
		auth = smtp.PlainAuth("", *n.SMTPUser, password, *n.SMTPHost)
	}
	// smtp.SendMail dials with no deadline, so a host that accepts the TCP
	// connection and then says nothing -- a firewalled address, a wrong port --
	// hung this goroutine forever. It is called synchronously from the job
	// completion path, so that also meant no Telegram message and no webhook
	// for that job, and one leaked goroutine per job after it.
	conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(smtpTimeout)); err != nil {
		return err
	}

	c, err := smtp.NewClient(conn, *n.SMTPHost)
	if err != nil {
		return err
	}
	defer c.Close()

	// STARTTLS when the server offers it, matching what the Python side asked
	// for explicitly.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: *n.SMTPHost}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(*n.ToEmail); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Scheduler runs saved cron schedules.
type Scheduler struct {
	DB      *store.DB
	Starter *Starter

	mu   sync.Mutex
	cron *cron.Cron
}

func (s *Scheduler) Start() {
	s.Reload()
}

// Reload rebuilds the cron table from the database. Cheap enough to do on
// every schedule change, and it removes a whole class of drift between what is
// stored and what is armed.
func (s *Scheduler) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron != nil {
		s.cron.Stop()
	}
	// In UTC+7, the zone every timestamp in this application is written in.
	// cron.New() defaults to time.Local, and the unit sets no TZ — so on a UTC
	// host a user's "0 2 * * *" fired at 09:00 Vietnam time, in the middle of
	// the working day, against production mail servers.
	// SkipIfStillRunning: cron does not skip a tick because the previous one is
	// still going, and this launches a mailbox sync. A profile on "*/30 * * * *"
	// whose sync takes two hours accumulated four concurrent imapsync processes
	// against the same mailbox pair -- connection limits on both servers and
	// cache contention, for no gain.
	s.cron = cron.New(
		cron.WithLocation(store.VN),
		cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
	)

	schedules, err := s.DB.ActiveSchedules()
	if err != nil {
		log.Printf("scheduler: %v", err)
		return
	}
	for _, sc := range schedules {
		sc := sc
		if _, err := s.cron.AddFunc(sc.CronExpression, func() { s.run(sc.ID) }); err != nil {
			log.Printf("scheduler: schedule %d has an invalid expression %q: %v",
				sc.ID, sc.CronExpression, err)
		}
	}
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil {
		s.cron.Stop()
	}
}

func (s *Scheduler) run(scheduleID int64) {
	sc, err := s.DB.ScheduleByIDAny(scheduleID)
	if err != nil || !sc.IsActive {
		return
	}
	_ = s.DB.TouchScheduleRun(sc.ID, nil)
	if _, err := s.Starter.StartProfile(sc.UserID, sc.ProfileID, nil, false, nil); err != nil {
		// A deleted profile leaves the schedule pointing at nothing; log it
		// rather than taking the cron goroutine down.
		log.Printf("scheduled sync for schedule %d: %v", sc.ID, err)
	}
}

// Ensure the concrete types satisfy what the API expects.
var (
	_ api.Starter     = (*Starter)(nil)
	_ api.Mailer      = (*Mailer)(nil)
	_ api.CronService = (*Scheduler)(nil)
)

// smtpTimeout bounds every stage of a mail delivery. A notification is not
// worth holding a goroutine open for.
const smtpTimeout = 20 * time.Second
