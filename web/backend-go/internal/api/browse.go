package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// Listing a large mailbox's folders takes minutes — longer than most clients
// hold a request open, and far longer than an MCP tool call is allowed. With
// "async": true, POST /folders/browse answers at once with a task, and the
// caller polls GET /folders/browse/{id} for the folders.

// browseKeep is how long a finished listing stays fetchable.
const browseKeep = 15 * time.Minute

type browseTask struct {
	ID      string       `json:"id"`
	Status  string       `json:"status"` // running | done | failed
	Folders []FolderInfo `json:"folders,omitempty"`
	Error   string       `json:"error,omitempty"`

	userID   int64
	key      string
	finished time.Time
}

type browseTasks struct {
	mu    sync.Mutex
	tasks map[string]*browseTask
}

var browseJobs = &browseTasks{tasks: map[string]*browseTask{}}

// start runs list in the background and returns the task tracking it. A
// listing of the same mailbox already running for the same user is returned
// instead of starting a second one: a client that retries after its own
// timeout should not pile up imapsync processes against one server.
func (b *browseTasks) start(userID int64, mailbox string, list func() ([]FolderInfo, error)) browseTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune()

	for _, t := range b.tasks {
		if t.userID == userID && t.key == mailbox && t.Status == "running" {
			return *t
		}
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	t := &browseTask{ID: hex.EncodeToString(buf), Status: "running", userID: userID, key: mailbox}
	b.tasks[t.ID] = t

	go func() {
		folders, err := list()
		b.mu.Lock()
		defer b.mu.Unlock()
		if err != nil {
			t.Status, t.Error = "failed", err.Error()
		} else {
			t.Status, t.Folders = "done", folders
		}
		t.finished = time.Now()
	}()
	return *t
}

// get returns a copy, so the caller can encode it without holding the lock.
// Another user's task is reported as missing, not forbidden.
func (b *browseTasks) get(userID int64, id string) (browseTask, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune()
	t, ok := b.tasks[id]
	if !ok || t.userID != userID {
		return browseTask{}, false
	}
	return *t, true
}

func (b *browseTasks) prune() {
	for id, t := range b.tasks {
		if t.Status != "running" && time.Since(t.finished) > browseKeep {
			delete(b.tasks, id)
		}
	}
}

func (s *Server) browseResult(w http.ResponseWriter, r *http.Request) {
	t, ok := browseJobs.get(userFrom(r).ID, chiURLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Folder listing not found — it may have expired")
		return
	}
	writeJSON(w, http.StatusOK, t)
}
