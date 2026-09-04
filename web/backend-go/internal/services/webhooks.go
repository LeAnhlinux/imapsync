package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

// Webhooks delivers job events to the URLs a user registered.
//
// Deliveries are signed with HMAC-SHA256 over the exact request body, so a
// receiver can tell a real call from anything else that finds the URL.
type Webhooks struct {
	DB  *store.DB
	Sec *security.Manager

	client *http.Client
}

func NewWebhooks(db *store.DB, sec *security.Manager) *Webhooks {
	return &Webhooks{DB: db, Sec: sec, client: &http.Client{
		Timeout: 10 * time.Second,
		// The destination is user-supplied, and this process sits on the same
		// host as the database and the key that decrypts every stored mailbox
		// password. Without a check, a webhook pointed at 127.0.0.1 or at
		// 169.254.169.254 turns "test this hook" into a port scanner and a
		// metadata-service fetcher, run from inside the trust boundary.
		//
		// Checking the address at dial time, rather than the hostname up front,
		// is what closes DNS rebinding and redirect chains: every connection
		// this client makes -- original or redirected -- passes through here.
		Transport: &http.Transport{
			DialContext: dialPublicOnly,
			// Redirects to a private address are refused by the dialer above,
			// but cap the chain anyway.
			MaxIdleConnsPerHost: 2,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}}
}

// Payload is the JSON body of a delivery.
type Payload struct {
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
	Job       struct {
		ID                  int64  `json:"id"`
		Status              string `json:"status"`
		SourceAccount       string `json:"source_account"`
		DestAccount         string `json:"dest_account"`
		MessagesTransferred int    `json:"messages_transferred"`
		BytesTransferred    int64  `json:"bytes_transferred"`
		ErrorsCount         int    `json:"errors_count"`
		ExitCode            *int   `json:"exit_code"`
		StartedAt           string `json:"started_at,omitempty"`
		FinishedAt          string `json:"finished_at,omitempty"`
		Summary             string `json:"summary,omitempty"`
	} `json:"job"`
}

// Dispatch sends one event to every active webhook subscribed to it.
func (wh *Webhooks) Dispatch(event string, userID, jobID int64) {
	hooks, secrets, err := wh.DB.ActiveWebhooks(userID)
	if err != nil {
		log.Printf("webhooks: %v", err)
		return
	}
	if len(hooks) == 0 {
		return
	}

	payload, err := wh.payloadFor(event, jobID)
	if err != nil {
		log.Printf("webhooks: %v", err)
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for i, hook := range hooks {
		if !hook.Subscribes(event) {
			continue
		}
		secret := ""
		if secrets[i] != "" {
			if secret, err = wh.Sec.Decrypt(secrets[i]); err != nil {
				log.Printf("webhooks: hook %d has an unreadable secret: %v", hook.ID, err)
				continue
			}
		}
		go wh.deliver(hook.ID, hook.URL, secret, event, body)
	}
}

func (wh *Webhooks) payloadFor(event string, jobID int64) (*Payload, error) {
	job, err := wh.DB.JobByID(jobID)
	if err != nil {
		return nil, err
	}
	var p Payload
	p.Event = event
	p.Timestamp = store.NowVN().Format(time.RFC3339)
	p.Job.ID = job.ID
	p.Job.Status = job.Status
	p.Job.SourceAccount = deref(job.SourceAccount)
	p.Job.DestAccount = deref(job.DestAccount)
	p.Job.MessagesTransferred = job.MessagesTransferred
	p.Job.BytesTransferred = job.BytesTransferred
	p.Job.ErrorsCount = job.ErrorsCount
	p.Job.ExitCode = job.ExitCode
	// The stored value is a naive UTC+7 wall clock that the sqlite driver hands
	// back tagged UTC. Formatting it straight to RFC3339 emitted "...Z" and told
	// every receiver the time was UTC — seven hours off. Re-stamp it into the
	// zone it was actually written in, so the offset in the payload is true.
	if job.StartedAt != nil {
		p.Job.StartedAt = store.InVN(job.StartedAt.Time).Format(time.RFC3339)
	}
	if job.FinishedAt != nil {
		p.Job.FinishedAt = store.InVN(job.FinishedAt.Time).Format(time.RFC3339)
	}
	p.Job.Summary = deref(job.Summary)
	return &p, nil
}

// deliver posts once, retrying a couple of times with backoff. A receiver that
// is down for longer than that is not worth blocking a goroutine over — the
// job's outcome is in the database and the API either way.
func (wh *Webhooks) deliver(hookID int64, url, secret, event string, body []byte) {
	backoff := time.Second
	var lastStatus int

	for attempt := 1; attempt <= 3; attempt++ {
		status, err := wh.post(url, secret, event, body)
		lastStatus = status
		if err == nil && status >= 200 && status < 300 {
			_ = wh.DB.RecordWebhookCall(hookID, status)
			return
		}
		if err != nil {
			// Not %v on the error: a *url.Error carries the whole URL, and a
			// Slack- or Discord-style webhook keeps its secret in the path. The
			// hook id is enough to find the row.
			log.Printf("webhooks: hook %d attempt %d: %v", hookID, attempt, bareError(err))
		}
		if attempt < 3 {
			time.Sleep(backoff)
			backoff *= 3
		}
	}
	_ = wh.DB.RecordWebhookCall(hookID, lastStatus)
	log.Printf("webhooks: hook %d gave up after 3 attempts (last status %d)", hookID, lastStatus)
}

func (wh *Webhooks) post(url, secret, event string, body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "imapsync-web/1.0")
	req.Header.Set("X-Imapsync-Event", event)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Imapsync-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := wh.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Test sends a synthetic delivery so a receiver can be checked from the UI.
func (wh *Webhooks) Test(hookID int64) (int, error) {
	enc, err := wh.DB.WebhookSecret(hookID)
	if err != nil {
		return 0, err
	}
	secret := ""
	if enc != "" {
		if secret, err = wh.Sec.Decrypt(enc); err != nil {
			return 0, err
		}
	}

	// Read the row directly: Test is called for a specific id the caller
	// already proved it owns.
	hook, err := wh.hookByID(hookID)
	if err != nil {
		return 0, err
	}

	body, _ := json.Marshal(map[string]any{
		"event":     "test",
		"timestamp": store.NowVN().Format(time.RFC3339),
		"message":   "This is a test delivery from imapsync Web UI.",
	})
	status, err := wh.post(hook.URL, secret, "test", body)
	if err != nil {
		return 0, err
	}
	_ = wh.DB.RecordWebhookCall(hookID, status)
	if status < 200 || status >= 300 {
		return status, fmt.Errorf("receiver answered HTTP %d", status)
	}
	return status, nil
}

func (wh *Webhooks) hookByID(id int64) (*store.Webhook, error) {
	var url string
	if err := wh.DB.QueryRow(`SELECT url FROM webhooks WHERE id=?`, id).Scan(&url); err != nil {
		return nil, err
	}
	return &store.Webhook{ID: id, URL: url}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var allowPrivateWebhooks = os.Getenv("WEBHOOKS_ALLOW_PRIVATE") == "true"

// dialPublicOnly refuses to connect to an address that is not routable on the
// public internet. It runs after DNS resolution, on the address actually being
// dialled, so a hostname that resolves to a private range -- deliberately or
// through a rebinding attack -- is stopped here rather than at validation time.
//
// An operator running this on a private network may genuinely want to deliver
// to a host on it. WEBHOOKS_ALLOW_PRIVATE=true turns the check off; it is off
// by default because the failure mode of the other default is a stranger
// reading the cloud metadata service through your own server.
func dialPublicOnly(ctx context.Context, network, addr string) (net.Conn, error) {
	if allowPrivateWebhooks {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	d := &net.Dialer{Timeout: 5 * time.Second}
	var lastErr error = fmt.Errorf("no usable address for %s", host)
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			lastErr = fmt.Errorf(
				"refusing to deliver to %s: %s is not a public address", host, ip.IP)
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10, the carrier-grade NAT range, is not covered by IsPrivate
	// and is where a good deal of cloud internal addressing lives.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
	}
	return true
}

// bareError strips the URL that net/http wraps around a transport failure.
func bareError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}
