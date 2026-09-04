package services

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imapsync/web-backend/internal/api"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

// Row is one mailbox pair from a batch CSV.
type Row struct {
	Host1, User1, Password1 string
	Host2, User2, Password2 string
	ExtraOptions            map[string]any
}

// ParseCSV reads the imapsync batch format:
//
//	host1;user1;password1;host2;user2;password2[;extra options]
//
// Lines starting with # are comments; short or empty lines are skipped.
func ParseCSV(text string) ([]Row, error) {
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1 // rows may or may not carry the options column
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read CSV: %w", err)
	}

	var rows []Row
	for _, fields := range records {
		if len(fields) == 0 || strings.HasPrefix(strings.TrimSpace(fields[0]), "#") {
			continue
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) < 6 {
			continue
		}
		r := Row{
			Host1: fields[0], User1: fields[1], Password1: fields[2],
			Host2: fields[3], User2: fields[4], Password2: fields[5],
		}
		if r.Host1 == "" || r.User1 == "" || r.Password1 == "" ||
			r.Host2 == "" || r.User2 == "" || r.Password2 == "" {
			continue
		}
		if len(fields) > 6 && fields[6] != "" {
			r.ExtraOptions = ParseOptionString(fields[6])
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// ParseOptionString reads "--flag --key value" into a map.
func ParseOptionString(s string) map[string]any {
	out := map[string]any{}
	parts := strings.Fields(s)
	for i := 0; i < len(parts); {
		if !strings.HasPrefix(parts[i], "--") {
			i++
			continue
		}
		key := strings.TrimLeft(parts[i], "-")
		if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "--") {
			out[key] = parts[i+1]
			i += 2
		} else {
			out[key] = true
			i++
		}
	}
	return out
}

// Batch creates and runs batch imports.
type Batch struct {
	DB      *store.DB
	Sec     *security.Manager
	Starter *Starter
	Runner  *runner.Runner

	// Cancel functions for batches currently running, so a long import can be
	// stopped without restarting the service.
	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

// Create parses the CSV into a pending batch. Passwords are encrypted before
// they are stored: batch_data lives in the database until the run finishes.
func (b *Batch) Create(userID int64, csvText, filename string, opts map[string]any) (*store.BatchRun, error) {
	rows, err := ParseCSV(csvText)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("No valid rows found in CSV data")
	}
	return b.CreateFromRows(userID, rows, filename, opts)
}

// CreateFromRows is for callers that did their own parsing, such as the
// Telegram /run command.
func (b *Batch) CreateFromRows(userID int64, rows []Row, filename string, opts map[string]any) (*store.BatchRun, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("No mailboxes to run")
	}

	stored := make([]any, 0, len(rows))
	for i, r := range rows {
		// Report rejected options instead of dropping them silently downstream.
		if _, rejected := runner.SanitizeOptions(r.ExtraOptions); len(rejected) > 0 {
			return nil, fmt.Errorf("Row %d: unsupported or unsafe imapsync options: %s",
				i+1, strings.Join(rejected, ", "))
		}
		enc1, err := b.Sec.Encrypt(r.Password1)
		if err != nil {
			return nil, err
		}
		enc2, err := b.Sec.Encrypt(r.Password2)
		if err != nil {
			return nil, err
		}
		stored = append(stored, map[string]any{
			"host1": r.Host1, "user1": r.User1, "password1": enc1,
			"host2": r.Host2, "user2": r.User2, "password2": enc2,
			"extra_options": r.ExtraOptions,
		})
	}

	data := store.JSONMap{"rows": stored}
	for k, v := range opts {
		data[k] = v
	}

	batch := &store.BatchRun{
		UserID: userID, Filename: filename,
		TotalRows: len(rows), Status: "pending", BatchData: data,
	}
	if err := b.DB.CreateBatch(batch); err != nil {
		return nil, err
	}
	return b.DB.BatchByID(batch.ID, userID)
}

// Start runs the batch in the background, one mailbox at a time.
func (b *Batch) Start(batchID, userID int64, data store.JSONMap) {
	ctx, cancel := context.WithCancel(context.Background())

	b.mu.Lock()
	if b.running == nil {
		b.running = map[int64]context.CancelFunc{}
	}
	b.running[batchID] = cancel
	b.mu.Unlock()

	go func() {
		defer func() {
			b.mu.Lock()
			delete(b.running, batchID)
			b.mu.Unlock()
			cancel()
		}()
		b.run(ctx, batchID, userID, data)
	}()
}

// Cancel stops a running batch after the mailbox in flight, and aborts that
// mailbox too. Reports whether the batch was actually running.
func (b *Batch) Cancel(batchID int64) bool {
	b.mu.Lock()
	cancel, ok := b.running[batchID]
	b.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

func (b *Batch) run(ctx context.Context, batchID, userID int64, data store.JSONMap) {
	rows, _ := data["rows"].([]any)
	dryRun := jsonBool(data["dry_run"], false)
	ssl1 := jsonBool(data["ssl1"], true)
	ssl2 := jsonBool(data["ssl2"], true)
	automap := jsonBool(data["automap"], true)
	usecache := jsonBool(data["usecache"], true)

	for _, raw := range rows {
		// Checked before each mailbox: a cancelled batch stops here rather
		// than working through the rest of the list.
		if ctx.Err() != nil {
			break
		}

		row, ok := raw.(map[string]any)
		if !ok {
			_ = b.DB.BumpBatchCounts(batchID, 0, 1)
			continue
		}

		extra := map[string]any{}
		if e, ok := row["extra_options"].(map[string]any); ok {
			for k, v := range e {
				extra[k] = v
			}
		}
		if automap {
			extra["automap"] = true
		}
		if usecache {
			extra["usecache"] = true
		}

		pw1, err1 := b.Sec.Decrypt(str(row["password1"]))
		pw2, err2 := b.Sec.Decrypt(str(row["password2"]))
		if err1 != nil || err2 != nil {
			log.Printf("batch %d: could not decrypt a row's credentials", batchID)
			_ = b.DB.BumpBatchCounts(batchID, 0, 1)
			continue
		}

		jobID, err := b.Starter.StartAdhoc(userID, api.AdhocParams{
			Host1: str(row["host1"]), User1: str(row["user1"]), Password1: pw1,
			Host2: str(row["host2"]), User2: str(row["user2"]), Password2: pw2,
			SSL1: ssl1, SSL2: ssl2, ExtraOptions: extra, DryRun: dryRun,
		})
		if err != nil {
			_ = b.DB.BumpBatchCounts(batchID, 0, 1)
			continue
		}

		// One mailbox at a time: several concurrent imapsync processes against
		// the same server tend to hit connection limits.
		if job := b.Runner.Get(jobID); job != nil {
			done := make(chan struct{})
			go func() { job.Wait(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				// Stop the mailbox in flight too; waiting for a 9,000-message
				// sync to finish is not what "cancel" means.
				b.Runner.Abort(jobID)
				_ = b.DB.MarkAborted(jobID)
				<-done
			}
		}
		if ctx.Err() != nil {
			_ = b.DB.BumpBatchCounts(batchID, 0, 1)
			break
		}

		// "It ran" is not "it worked". This counted every mailbox that merely
		// started as completed, so a CSV where every password was wrong reported
		// completed_rows=10, failed_rows=0 — and the operator believed ten
		// mailboxes had migrated.
		if b.jobSucceeded(jobID) {
			_ = b.DB.BumpBatchCounts(batchID, 1, 0)
		} else {
			_ = b.DB.BumpBatchCounts(batchID, 0, 1)
		}
	}

	// Clearing batch_data is part of finishing: it holds credentials.
	if ctx.Err() != nil {
		if err := b.DB.AbortBatch(batchID); err != nil {
			log.Printf("batch %d: %v", batchID, err)
		}
		return
	}
	if err := b.DB.FinishBatch(batchID); err != nil {
		log.Printf("batch %d: %v", batchID, err)
	}
}

func jsonBool(v any, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// Listing folders means SELECTing every one of them. On a Gmail account with
// ~9k messages this was measured at four to five minutes, so the old 60s ceiling
// meant the browser never returned anything for a real mailbox.
const browseTimeout = 6 * time.Minute

// Folders lists a mailbox's folders with imapsync --justfoldersizes.
type Folders struct {
	ImapsyncPath string
	TmpDir       string
}

// imapsync --justfoldersizes prints one aligned line per folder per host:
//
//	Host1 folder     4/5 [Sent Mail]      Size:      0 Messages:     0 Biggest:      0
//
// Only Host1 is read — browsing looks at one mailbox, and the runner passes the
// same host twice to satisfy imapsync's two-sided interface. The name is taken
// non-greedily up to the closing bracket because folder names contain spaces.
var folderSizePat = regexp.MustCompile(
	`(?m)^Host1 folder\s+\d+/\d+\s+\[(.*?)\]\s+Size:\s+(\d+)\s+Messages:\s+(\d+)`)

func (f *Folders) Browse(host, user, password string, ssl bool) ([]api.FolderInfo, error) {
	passfile, err := os.CreateTemp(f.TmpDir, "imapsync_browse_")
	if err != nil {
		return nil, err
	}
	name := passfile.Name()
	// Removed only after the process exits: imapsync opens the file after it
	// starts, so an earlier unlink races the child and the login fails.
	defer os.Remove(name)
	if err := passfile.Chmod(0o600); err != nil {
		passfile.Close()
		return nil, err
	}
	if _, err := passfile.WriteString(password); err != nil {
		passfile.Close()
		return nil, err
	}
	passfile.Close()

	args := []string{
		"--host1", host, "--user1", user, "--passfile1", name,
		// host2 is required even though nothing is written in --dry mode.
		"--host2", host, "--user2", user, "--passfile2", name,
		"--justfoldersizes", "--dry", "--nolog",
	}
	if ssl {
		args = append(args, "--ssl1", "--ssl2")
	}

	cmd := exec.Command(f.ImapsyncPath, args...)
	done := make(chan struct{})
	var out []byte
	var runErr error
	go func() {
		out, runErr = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(browseTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return nil, fmt.Errorf("timed out after %s talking to %s. Large mailboxes can take several minutes to list",
			browseTimeout, host)
	}

	folders := parseFolderSizes(string(out))
	if len(folders) == 0 && runErr != nil {
		return nil, fmt.Errorf("imapsync could not list folders: %w", runErr)
	}
	return folders, nil
}

func parseFolderSizes(output string) []api.FolderInfo {
	seen := map[string]bool{}
	var folders []api.FolderInfo
	for _, m := range folderSizePat.FindAllStringSubmatch(output, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		size, _ := strconv.ParseInt(m[2], 10, 64)
		messages, _ := strconv.Atoi(m[3])
		folders = append(folders, api.FolderInfo{Name: name, Messages: messages, Size: size})
	}
	return folders
}

var (
	_ api.BatchService  = (*Batch)(nil)
	_ api.FolderBrowser = (*Folders)(nil)
)

// jobSucceeded reports whether a finished mailbox actually copied cleanly.
// A job the completion handler has not written yet reads as running; treat that
// as success, since the alternative is calling a good run failed on a race.
func (b *Batch) jobSucceeded(jobID int64) bool {
	job, err := b.DB.JobByID(jobID)
	if err != nil {
		return false
	}
	switch job.Status {
	case "failed", "aborted", "interrupted":
		return false
	}
	return true
}
