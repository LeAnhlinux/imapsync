// Package runner spawns imapsync and turns its output into progress events.
package runner

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imapsync/web-backend/internal/store"
)

// Event is one message pushed to WebSocket subscribers.
type Event struct {
	Type     string  `json:"type"` // log | progress | status
	Data     string  `json:"data,omitempty"`
	Percent  float64 `json:"percent,omitempty"`
	ETA      string  `json:"eta,omitempty"`
	Folder   string  `json:"folder,omitempty"`
	Status   string  `json:"status,omitempty"`
	ExitCode int     `json:"exit_code,omitempty"`
	Messages int     `json:"messages,omitempty"`
	Bytes    int64   `json:"bytes,omitempty"`
}

type Progress struct {
	Percent  float64
	Folder   string
	ETA      string
	Messages int
	Bytes    int64
}

const maxLogLines = 10000

// Job is a live imapsync process.
type Job struct {
	ID  int64
	cmd *exec.Cmd

	mu          sync.Mutex
	lines       []string
	progress    Progress
	aborted     bool
	subscribers map[int]chan Event
	nextSub     int
	done        chan struct{}

	// The log is appended as it arrives rather than written once at exit, so a
	// running job has a readable log and a long run is not truncated to the last
	// maxLogLines. lines stays as the tail kept for WebSocket replay.
	logFile *os.File
}

func (j *Job) Progress() Progress {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.progress
}

func (j *Job) Log() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return strings.Join(j.lines, "\n")
}

// Subscribe returns a channel of events plus a cancel function. The channel is
// buffered and dropped-on-full: a stalled browser must not block the reader
// goroutine that is draining imapsync's stdout.
func (j *Job) Subscribe() (<-chan Event, func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	id := j.nextSub
	j.nextSub++
	ch := make(chan Event, 256)
	j.subscribers[id] = ch
	return ch, func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		if c, ok := j.subscribers[id]; ok {
			delete(j.subscribers, id)
			close(c)
		}
	}
}

// Replay returns the log so far plus the current progress, for a client that
// connects mid-run.
func (j *Job) Replay() []Event {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Event, 0, len(j.lines)+1)
	for _, l := range j.lines {
		out = append(out, Event{Type: "log", Data: l})
	}
	if j.progress.Percent > 0 || j.progress.Messages > 0 || j.progress.Folder != "" {
		out = append(out, Event{Type: "progress", Percent: j.progress.Percent,
			Folder: j.progress.Folder, ETA: j.progress.ETA,
			Messages: j.progress.Messages, Bytes: j.progress.Bytes})
	}
	return out
}

func (j *Job) publish(e Event) {
	for _, ch := range j.subscribers {
		select {
		case ch <- e:
		default: // slow consumer: drop rather than stall the reader
		}
	}
}

// Runner owns every live job.
type Runner struct {
	ImapsyncPath string
	TmpDir       string
	CacheDir     string
	LogDir       string

	mu   sync.RWMutex
	jobs map[int64]*Job

	// OnProgress is called at most once a second while a job runs, and once more
	// when it ends. Listeners decide for themselves how often that is worth
	// acting on — the runner does not want to know what a "milestone" means to
	// each of them.
	OnProgress func(jobID int64, p Progress)
	// OnComplete records the outcome once the process exits.
	OnComplete func(jobID int64, exitCode int, summary store.Summary)
}

func New(imapsyncPath, tmpDir, cacheDir, logDir string) *Runner {
	return &Runner{
		ImapsyncPath: imapsyncPath, TmpDir: tmpDir, CacheDir: cacheDir, LogDir: logDir,
		jobs: map[int64]*Job{},
	}
}

func (r *Runner) Get(id int64) *Job {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.jobs[id]
}

// Params describes one imapsync invocation.
type Params struct {
	Host1, User1, Password1 string
	Host2, User2, Password2 string
	SSL1, SSL2              bool
	Folders                 []string
	ExtraOptions            map[string]any
	DryRun                  bool
}

// Start launches imapsync for a job and begins streaming its output.
func (r *Runner) Start(jobID int64, p Params) (*Job, error) {
	// Passwords go through files, not argv: argv is world-readable in ps.
	pass1, err := writePassfile(r.TmpDir, "p1", p.Password1)
	if err != nil {
		return nil, err
	}
	pass2, err := writePassfile(r.TmpDir, "p2", p.Password2)
	if err != nil {
		os.Remove(pass1)
		return nil, err
	}

	args := []string{
		"--host1", p.Host1, "--user1", p.User1, "--passfile1", pass1,
		"--host2", p.Host2, "--user2", p.User2, "--passfile2", pass2,
		"--nolog",
		"--tmpdir", r.TmpDir,
		"--pidfile", filepath.Join(r.TmpDir, fmt.Sprintf("imapsync_%d.pid", jobID)),
	}
	if p.SSL1 {
		args = append(args, "--ssl1")
	}
	if p.SSL2 {
		args = append(args, "--ssl2")
	}
	if p.DryRun {
		args = append(args, "--dry")
	}
	for _, f := range p.Folders {
		args = append(args, "--folder", f)
	}

	// Anything outside the allowlist is dropped here too, not just at the API
	// boundary: this is the single place every caller funnels through.
	safe, rejected := SanitizeOptions(p.ExtraOptions)
	for k, v := range safe {
		if v == true {
			args = append(args, "--"+k)
		} else {
			args = append(args, "--"+k, fmt.Sprintf("%v", v))
		}
	}

	cmd := exec.Command(r.ImapsyncPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		os.Remove(pass1)
		os.Remove(pass2)
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	job := &Job{ID: jobID, cmd: cmd, subscribers: map[int]chan Event{}, done: make(chan struct{})}
	if r.LogDir != "" {
		// A job whose log cannot be opened still runs — losing the transcript is
		// worse than nothing, but refusing to sync over it would be worse still.
		f, err := os.OpenFile(filepath.Join(r.LogDir, fmt.Sprintf("%d.txt", jobID)),
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err == nil {
			job.logFile = f
		}
	}
	if len(rejected) > 0 {
		warning := "Warning: ignored unsupported or unsafe options: " + strings.Join(rejected, ", ")
		job.lines = append(job.lines, warning)
		if job.logFile != nil {
			_, _ = job.logFile.WriteString(warning + "\n")
		}
	}

	if err := cmd.Start(); err != nil {
		os.Remove(pass1)
		os.Remove(pass2)
		if job.logFile != nil {
			job.logFile.Close()
		}
		return nil, err
	}

	r.mu.Lock()
	r.jobs[jobID] = job
	r.mu.Unlock()

	go r.stream(job, stdout, []string{pass1, pass2})
	return job, nil
}

func (j *Job) PID() int {
	if j.cmd.Process == nil {
		return 0
	}
	return j.cmd.Process.Pid
}

// Wait blocks until the job's process exits. Used by the batch runner, which
// runs one mailbox at a time.
func (j *Job) Wait() { <-j.done }

// What imapsync actually prints. Verified against a 9,802-line log from a real
// run, not inferred: an earlier version of this file looked for
// "Syncing folder [x] (n/m)", which appears nowhere in imapsync's output, so
// progress sat at zero for every job ever run.
var (
	// Appended to every ETA line and to the tail of every copy line:
	//   ETA: Wednesday 29 July 2026-07-29 12:48:51 +0700 +07  9255 s  9175/18945 msgs left
	// The pair is remaining/total, so what is done is total minus remaining.
	msgsLeftPat = regexp.MustCompile(`(\d+)/(\d+) msgs left`)
	etaSecsPat  = regexp.MustCompile(`ETA:.*?\s(\d+) s\s`)
	// One line per message actually written, the braces holding its size:
	//   msg INBOX/42 {1234}          copied to INBOX/57  0.50 msgs/s ...
	copiedPat = regexp.MustCompile(`^msg .*\{(\d+)\}\s+copied to `)
	// Sizing walks the folders before the first message moves, and on a large
	// account that takes minutes. Without it the UI would show nothing at all
	// for the part of the run users are most likely to be watching.
	//   Host1 folder    4/15 [Deleted Items]   Size: 2239 Messages: 3 ...
	sizingPat = regexp.MustCompile(`^Host1 folder\s+(\d+)/(\d+)\s+\[`)

	transferPat = regexp.MustCompile(`Messages transferred\s*:\s*(\d+)`)
	bytesPat    = regexp.MustCompile(`Total bytes transferred\s*:\s*(\d+)`)
	errorsPat   = regexp.MustCompile(`Detected\s+(\d+)\s+errors`)
)

// scanLine folds one line of imapsync output into the running progress and
// reports whether anything changed. Split out from the reader loop so it can be
// tested against captured output without spawning a process.
func scanLine(line string, p *Progress) bool {
	changed := false

	if m := copiedPat.FindStringSubmatch(line); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		p.Messages++
		p.Bytes += n
		changed = true
	}
	if m := sizingPat.FindStringSubmatch(line); m != nil {
		p.Folder = "sizing " + m[1] + "/" + m[2]
		changed = true
	}
	if m := msgsLeftPat.FindStringSubmatch(line); m != nil {
		left, _ := strconv.Atoi(m[1])
		total, _ := strconv.Atoi(m[2])
		if total > 0 {
			pct := float64(total-left) / float64(total) * 100
			p.Percent = float64(int(pct*10+0.5)) / 10
			p.Folder = Comma(int64(total-left)) + "/" + Comma(int64(total)) + " messages"
			changed = true
		}
	}
	if m := etaSecsPat.FindStringSubmatch(line); m != nil {
		secs, _ := strconv.Atoi(m[1])
		p.ETA = (time.Duration(secs) * time.Second).String()
		changed = true
	}
	return changed
}

func (r *Runner) stream(job *Job, stdout interface{ Read([]byte) (int, error) }, passfiles []string) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// A copy line per message means progress changes thousands of times. Both
	// the DB write and the WebSocket push are throttled to something a human can
	// read; the counters themselves stay exact.
	const publishEvery = time.Second
	var lastPublish time.Time

	for scanner.Scan() {
		line := scanner.Text()

		job.mu.Lock()
		job.lines = append(job.lines, line)
		if len(job.lines) > maxLogLines {
			job.lines = job.lines[len(job.lines)-maxLogLines:]
		}
		if job.logFile != nil {
			_, _ = job.logFile.WriteString(line + "\n")
		}
		job.publish(Event{Type: "log", Data: line})

		changed := scanLine(line, &job.progress)
		p := job.progress
		job.mu.Unlock()

		if changed && time.Since(lastPublish) >= publishEvery {
			lastPublish = time.Now()
			job.mu.Lock()
			job.publish(Event{Type: "progress", Percent: p.Percent, Folder: p.Folder,
				ETA: p.ETA, Messages: p.Messages, Bytes: p.Bytes})
			job.mu.Unlock()
			if r.OnProgress != nil {
				r.OnProgress(job.ID, p)
			}
		}
	}

	err := job.cmd.Wait()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Only now that the process is gone: imapsync reads the passfiles after it
	// starts, so removing them earlier races the child.
	for _, f := range passfiles {
		os.Remove(f)
	}

	job.mu.Lock()
	logText := strings.Join(job.lines, "\n")
	aborted := job.aborted
	final := job.progress
	if job.logFile != nil {
		job.logFile.Close()
		job.logFile = nil
	}
	job.mu.Unlock()

	// The throttle can swallow the last second of counting, and an aborted job
	// never reaches imapsync's summary block at all, so the tally observed line
	// by line is the only record of what it managed to move.
	if r.OnProgress != nil && final.Messages > 0 {
		r.OnProgress(job.ID, final)
	}

	summary := parseSummary(logText, exitCode, aborted)
	if summary.MessagesTransferred == 0 && final.Messages > 0 {
		summary.MessagesTransferred = final.Messages
		summary.BytesTransferred = final.Bytes
	}

	status := "completed"
	if aborted {
		status = "aborted"
	} else if exitCode != 0 {
		status = "failed"
	}

	job.mu.Lock()
	job.publish(Event{Type: "status", Status: status, ExitCode: exitCode})
	for id, ch := range job.subscribers {
		delete(job.subscribers, id)
		close(ch)
	}
	job.mu.Unlock()

	r.mu.Lock()
	delete(r.jobs, job.ID)
	r.mu.Unlock()

	close(job.done)

	if r.OnComplete != nil {
		r.OnComplete(job.ID, exitCode, summary)
	}
}

// Abort asks a running job to stop, escalating if it ignores SIGTERM.
func (r *Runner) Abort(jobID int64) bool {
	job := r.Get(jobID)
	if job == nil || job.cmd.Process == nil {
		return false
	}
	job.mu.Lock()
	job.aborted = true
	job.mu.Unlock()

	if err := job.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return false
	}
	go func() {
		select {
		case <-job.done:
		case <-time.After(5 * time.Second):
			_ = job.cmd.Process.Kill()
		}
	}()
	return true
}

// LogFor returns a job's log, running or finished. The file is preferred over
// the in-memory tail because the tail is capped and a real migration overruns
// it — a 9,000-message run would otherwise lose its opening, which is where the
// settings and folder mapping are recorded.
func (r *Runner) LogFor(jobID int64) string {
	if r.LogDir != "" {
		if b, err := os.ReadFile(filepath.Join(r.LogDir, fmt.Sprintf("%d.txt", jobID))); err == nil {
			return string(b)
		}
	}
	if job := r.Get(jobID); job != nil {
		return job.Log()
	}
	return ""
}

var (
	foldersPat  = regexp.MustCompile(`Folders synced\s*:\s*(\d+)/(\d+)`)
	skippedPat  = regexp.MustCompile(`Messages skipped\s*:\s*(\d+)`)
	leftOverPat = regexp.MustCompile(`Messages found in host1 not in host2\s*:\s*(\d+)`)
	durationPat = regexp.MustCompile(`Transfer time\s*:\s*([\d.]+) sec`)
	// imapsync prefixes its own diagnostics this way. They are the only lines
	// worth forwarding when a run goes wrong.
	failurePat = regexp.MustCompile(`(?m)^(?:Failure|Error|Err)\b[^\n]*`)
)

// exitReason turns imapsync's exit code into something a person can act on.
// The codes are documented in imapsync itself; the last two are the ones that
// actually come up, because a wrong password is the usual reason a sync dies.
func exitReason(code int) string {
	switch code {
	case 0:
		return ""
	case 6:
		return "Stopped by a signal"
	case 8:
		return "Could not write its pid file"
	case 10:
		return "Could not connect to a server"
	case 12:
		return "TLS negotiation failed"
	case 16:
		return "Authentication failed"
	case 21:
		return "The subfolder given for the source does not exist"
	case 101:
		return "Could not connect to the source server"
	case 102:
		return "Could not connect to the destination server"
	case 111, 112:
		return "Finished, but with errors"
	case 113:
		return "The destination mailbox is over quota"
	case 114:
		return "The destination server refused a message"
	case 115:
		return "Could not read a message from the source"
	case 116:
		return "Could not create a folder on the destination"
	case 117:
		return "Could not open a folder"
	case 118:
		return "The transfer limit was exceeded"
	case 119:
		return "The destination server rejected a message as a virus"
	case 161:
		return "Wrong username or password for the source account"
	case 162:
		return "Wrong username or password for the destination account"
	}
	return fmt.Sprintf("imapsync exited with code %d", code)
}

// Comma groups thousands: 14400 reads as a serial number often enough to be
// worth the twelve lines.
func Comma(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return "-" + Comma(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// HumanBytes renders a byte count the way imapsync does, in binary units.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// parseSummary reads imapsync's statistics block. It used to take the last five
// lines of the log instead, which are always the same housekeeping — the
// homepage URL, the pid file being removed — and said nothing about the run.
func parseSummary(text string, exitCode int, aborted bool) store.Summary {
	var s store.Summary
	s.Aborted = aborted

	if m := transferPat.FindStringSubmatch(text); m != nil {
		s.MessagesTransferred, _ = strconv.Atoi(m[1])
	}
	if m := bytesPat.FindStringSubmatch(text); m != nil {
		s.BytesTransferred, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := errorsPat.FindStringSubmatch(text); m != nil {
		s.ErrorsCount, _ = strconv.Atoi(m[1])
	}

	var parts []string
	if m := foldersPat.FindStringSubmatch(text); m != nil {
		parts = append(parts, fmt.Sprintf("%s/%s folders", m[1], m[2]))
	}
	if m := skippedPat.FindStringSubmatch(text); m != nil {
		if n, _ := strconv.ParseInt(m[1], 10, 64); n > 0 {
			parts = append(parts, Comma(n)+" already there")
		}
	}
	if m := leftOverPat.FindStringSubmatch(text); m != nil {
		if n, _ := strconv.ParseInt(m[1], 10, 64); n > 0 {
			parts = append(parts, Comma(n)+" still only on the source")
		}
	}
	if m := durationPat.FindStringSubmatch(text); m != nil {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil && secs >= 1 {
			d := time.Duration(secs * float64(time.Second)).Round(time.Second)
			parts = append(parts, "took "+d.String())
		}
	}

	var lines []string
	switch {
	case aborted:
		lines = append(lines, "Stopped before it finished. Messages already copied stay copied.")
	case exitCode != 0:
		lines = append(lines, exitReason(exitCode))
	}
	if len(parts) > 0 {
		lines = append(lines, strings.Join(parts, " · "))
	}
	if s.ErrorsCount > 0 {
		lines = append(lines, fmt.Sprintf("%d error(s) reported", s.ErrorsCount))
	}
	// Only on a bad run: on a good one these do not exist, and quoting log
	// lines at someone who asked for a result is noise.
	if exitCode != 0 {
		found := failurePat.FindAllString(text, -1)
		if len(found) > 3 {
			found = found[len(found)-3:]
		}
		lines = append(lines, found...)
	}

	s.Text = strings.TrimSpace(strings.Join(lines, "\n"))
	return s
}

func writePassfile(dir, prefix, password string) (string, error) {
	f, err := os.CreateTemp(dir, "imapsync_"+prefix+"_")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := f.WriteString(password); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// DeleteLog removes a job's saved output. Called when the job row goes away:
// the row is a few hundred bytes, the log can be megabytes.
func (r *Runner) DeleteLog(jobID int64) {
	_ = os.Remove(filepath.Join(r.LogDir, fmt.Sprintf("%d.txt", jobID)))
}
