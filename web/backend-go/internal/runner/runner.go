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
}

type Progress struct {
	Percent float64
	Folder  string
	ETA     string
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
	if j.progress.Percent > 0 {
		out = append(out, Event{Type: "progress", Percent: j.progress.Percent,
			Folder: j.progress.Folder, ETA: j.progress.ETA})
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

	// OnProgress is called for every progress update. Listeners decide for
	// themselves how often that is worth acting on — the runner does not want
	// to know what a "milestone" means to each of them.
	OnProgress func(jobID int64, percent float64, folder string)
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
	if len(rejected) > 0 {
		job.lines = append(job.lines,
			"Warning: ignored unsupported or unsafe options: "+strings.Join(rejected, ", "))
	}

	if err := cmd.Start(); err != nil {
		os.Remove(pass1)
		os.Remove(pass2)
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

var (
	folderPat   = regexp.MustCompile(`Syncing folder \[.*?\] \((\d+)/(\d+)\)`)
	etaPat      = regexp.MustCompile(`ETA:\s*(\S+)`)
	transferPat = regexp.MustCompile(`Messages transferred\s*:\s*(\d+)`)
	bytesPat    = regexp.MustCompile(`Total bytes transferred\s*:\s*(\d+)`)
	errorsPat   = regexp.MustCompile(`Detected\s+(\d+)\s+errors`)
)

func (r *Runner) stream(job *Job, stdout interface{ Read([]byte) (int, error) }, passfiles []string) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		job.mu.Lock()
		job.lines = append(job.lines, line)
		if len(job.lines) > maxLogLines {
			job.lines = job.lines[len(job.lines)-maxLogLines:]
		}
		job.publish(Event{Type: "log", Data: line})

		if m := folderPat.FindStringSubmatch(line); m != nil {
			cur, _ := strconv.Atoi(m[1])
			total, _ := strconv.Atoi(m[2])
			percent := 0.0
			if total > 0 {
				percent = float64(cur) / float64(total) * 100
			}
			percent = float64(int(percent*10+0.5)) / 10
			job.progress = Progress{Percent: percent, Folder: m[1] + "/" + m[2], ETA: job.progress.ETA}
			job.publish(Event{Type: "progress", Percent: percent,
				Folder: job.progress.Folder, ETA: job.progress.ETA})
			p := job.progress
			job.mu.Unlock()
			if r.OnProgress != nil {
				r.OnProgress(job.ID, p.Percent, p.Folder)
			}
			continue
		}
		if m := etaPat.FindStringSubmatch(line); m != nil {
			job.progress.ETA = m[1]
		}
		job.mu.Unlock()
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
	job.mu.Unlock()

	if r.LogDir != "" {
		_ = os.WriteFile(filepath.Join(r.LogDir, fmt.Sprintf("%d.txt", job.ID)), []byte(logText), 0o600)
	}

	summary := parseSummary(logText)
	summary.Aborted = aborted

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

// LogFor returns the live buffer for a running job, or the saved file.
func (r *Runner) LogFor(jobID int64) string {
	if job := r.Get(jobID); job != nil {
		return job.Log()
	}
	b, err := os.ReadFile(filepath.Join(r.LogDir, fmt.Sprintf("%d.txt", jobID)))
	if err != nil {
		return ""
	}
	return string(b)
}

func parseSummary(text string) store.Summary {
	var s store.Summary
	if m := transferPat.FindStringSubmatch(text); m != nil {
		s.MessagesTransferred, _ = strconv.Atoi(m[1])
	}
	if m := bytesPat.FindStringSubmatch(text); m != nil {
		s.BytesTransferred, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := errorsPat.FindStringSubmatch(text); m != nil {
		s.ErrorsCount, _ = strconv.Atoi(m[1])
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	s.Text = strings.Join(lines, "\n")
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
