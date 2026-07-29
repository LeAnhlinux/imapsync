// Package telegram runs a long-polling bot inside the API process.
//
// Only chat ids in the allowed list are answered — the bot can start syncs and
// read job summaries, so that list is the entire access control story. Message
// text is treated as data: nothing but the leading /command is interpreted, and
// no message content ever reaches a shell.
//
// Replies are plain text on purpose. Mailbox names and hosts are full of
// underscores and dots that Telegram's Markdown parser chokes on, and escaping
// them everywhere is a bug factory.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/imapsync/web-backend/internal/api"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/services"
	"github.com/imapsync/web-backend/internal/store"
)

const (
	pollTimeout = 30
	maxMessage  = 3500 // Telegram's limit is 4096; leave room for our framing
	maxCSVBytes = 1_000_000
)

const helpText = `imapsync bot

/profiles - list sync profiles
/sync <profile_id> - start a sync from a profile
/run - sync one mailbox now (send /run on its own for the format)
/batch - send a .csv file with this as the caption
/batchgo <batch_id> - run a batch you uploaded
/jobs - jobs running right now
/status <job_id> - status of one job
/stop <job_id> - abort a running job
/history - last 10 finished jobs
/help - this message`

const credentialWarning = "I deleted your message from this chat, but Telegram still has a copy on their servers."

type config struct {
	token            string
	chatIDs          []int64
	userID           int64
	notifyOnSuccess  bool
	notifyOnFailure  bool
	notifyOnProgress bool
	progressStep     int
}

func (c config) usable() bool {
	return c.token != "" && len(c.chatIDs) > 0 && c.userID != 0
}

// Bot owns the polling loop and the settings it was started with.
type Bot struct {
	DB      *store.DB
	Sec     *security.Manager
	Runner  *runner.Runner
	Starter *services.Starter
	Batch   *services.Batch

	mu       sync.RWMutex
	cfg      config
	cancel   context.CancelFunc
	running  bool
	username string
	lastErr  string

	milestoneMu sync.Mutex
	milestones  map[int64]int

	client *http.Client
}

func New(db *store.DB, sec *security.Manager, run *runner.Runner,
	starter *services.Starter, batch *services.Batch) *Bot {
	return &Bot{
		DB: db, Sec: sec, Runner: run, Starter: starter, Batch: batch,
		milestones: map[int64]int{},
		client:     &http.Client{Timeout: (pollTimeout + 15) * time.Second},
	}
}

// loadConfig reads the single settings row.
func (b *Bot) loadConfig() config {
	t, err := b.DB.TelegramSettings()
	if err != nil || !t.Enabled || t.BotTokenEnc == nil || t.UserID == nil {
		return config{}
	}
	token, err := b.Sec.Decrypt(*t.BotTokenEnc)
	if err != nil {
		log.Println("telegram: stored token could not be decrypted; bot stays off")
		return config{}
	}
	step := t.ProgressStep
	if step < 1 {
		step = 25
	}
	return config{
		token:            token,
		chatIDs:          api.ParseChatIDs(t.AllowedChatIDs),
		userID:           *t.UserID,
		notifyOnSuccess:  t.NotifyOnSuccess,
		notifyOnFailure:  t.NotifyOnFailure,
		notifyOnProgress: t.NotifyOnProgress,
		progressStep:     step,
	}
}

func (b *Bot) current() config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cfg
}

// Start loads the settings and begins polling, if the bot is enabled.
func (b *Bot) Start() {
	b.Stop()

	cfg := b.loadConfig()
	b.mu.Lock()
	b.cfg = cfg
	b.mu.Unlock()

	if !cfg.usable() {
		b.setStatus(false, "", "")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()

	go b.poll(ctx, cfg)
}

func (b *Bot) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	b.cancel = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	b.setStatus(false, "", "")
}

// Restart picks up settings changed from the Settings page without a service
// restart.
func (b *Bot) Restart() { b.Start() }

func (b *Bot) setStatus(running bool, username, errMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running, b.username, b.lastErr = running, username, errMsg
}

func (b *Bot) Status() map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var username, errMsg any
	if b.username != "" {
		username = b.username
	}
	if b.lastErr != "" {
		errMsg = b.lastErr
	}
	return map[string]any{
		"running":      b.running,
		"configured":   b.cfg.usable(),
		"bot_username": username,
		"error":        errMsg,
	}
}

// ------------------------------------------------------------- Telegram API

// redact hides the bot token, which Telegram client errors quote as part of
// the request URL.
func redact(err error, token string) string {
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "<token>")
	}
	return msg
}

func (b *Bot) call(ctx context.Context, token, method string, params map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%s: %s", method, resp.Status)
	}
	if !payload.OK {
		return nil, fmt.Errorf("%s: %s", method, payload.Description)
	}
	return payload.Result, nil
}

func (b *Bot) sendMessage(ctx context.Context, token string, chatID int64, text string) {
	if len(text) > maxMessage {
		text = text[:maxMessage] + "\n... (truncated)"
	}
	if _, err := b.call(ctx, token, "sendMessage", map[string]any{
		"chat_id": chatID, "text": text,
	}); err != nil {
		log.Printf("telegram: sendMessage to %d failed: %s", chatID, redact(err, token))
	}
}

// SendTest is used by the Settings page before the bot is switched on.
func (b *Bot) SendTest(token string, chatIDs []int64) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	me, err := b.call(ctx, token, "getMe", nil)
	if err != nil {
		return false, redact(err, token)
	}
	var who struct {
		Username string `json:"username"`
	}
	_ = json.Unmarshal(me, &who)

	for _, id := range chatIDs {
		if _, err := b.call(ctx, token, "sendMessage", map[string]any{
			"chat_id": id,
			"text":    "imapsync Web UI: test message. The bot can reach this chat.",
		}); err != nil {
			return false, redact(err, token)
		}
	}
	return true, "Sent as @" + who.Username
}

func (b *Bot) deleteMessage(ctx context.Context, token string, chatID int64, messageID int64) {
	if _, err := b.call(ctx, token, "deleteMessage", map[string]any{
		"chat_id": chatID, "message_id": messageID,
	}); err != nil {
		log.Printf("telegram: could not delete a message: %s", redact(err, token))
	}
}

func (b *Bot) downloadFile(ctx context.Context, token, fileID string) ([]byte, error) {
	res, err := b.call(ctx, token, "getFile", map[string]any{"file_id": fileID})
	if err != nil {
		return nil, err
	}
	var info struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	if err := json.Unmarshal(res, &info); err != nil {
		return nil, err
	}
	if info.FileSize > maxCSVBytes {
		return nil, fmt.Errorf("too large")
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, info.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxCSVBytes))
}

// ------------------------------------------------------------ polling loop

type update struct {
	UpdateID int64    `json:"update_id"`
	Message  *message `json:"message"`
	Edited   *message `json:"edited_message"`
}

type message struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text     string `json:"text"`
	Caption  string `json:"caption"`
	Document *struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
	} `json:"document"`
}

func (b *Bot) poll(ctx context.Context, cfg config) {
	me, err := b.call(ctx, cfg.token, "getMe", nil)
	if err != nil {
		msg := redact(err, cfg.token)
		b.setStatus(false, "", msg)
		log.Printf("telegram: could not authenticate, giving up: %s", msg)
		return
	}
	var who struct {
		Username string `json:"username"`
	}
	_ = json.Unmarshal(me, &who)
	b.setStatus(true, who.Username, "")
	log.Printf("telegram: connected as @%s", who.Username)

	var offset int64
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		params := map[string]any{"timeout": pollTimeout}
		if offset > 0 {
			params["offset"] = offset
		}
		res, err := b.call(ctx, cfg.token, "getUpdates", params)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			msg := redact(err, cfg.token)
			b.setStatus(true, who.Username, msg)
			log.Printf("telegram: polling error (retry in %s): %s", backoff, msg)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		b.setStatus(true, who.Username, "")

		var updates []update
		if err := json.Unmarshal(res, &updates); err != nil {
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.handle(ctx, cfg, &u)
		}
	}
}

func (b *Bot) handle(ctx context.Context, cfg config, u *update) {
	msg := u.Message
	if msg == nil {
		msg = u.Edited
	}
	if msg == nil {
		return
	}

	if !allowed(cfg.chatIDs, msg.Chat.ID) {
		log.Printf("telegram: ignoring message from unauthorized chat %d", msg.Chat.ID)
		return
	}
	if msg.Text == "" && msg.Caption == "" && msg.Document == nil {
		return
	}

	reply := b.dispatch(ctx, cfg, msg)
	if reply != "" {
		b.sendMessage(ctx, cfg.token, msg.Chat.ID, reply)
	}
}

func allowed(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
