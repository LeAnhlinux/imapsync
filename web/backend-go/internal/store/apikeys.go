package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// APIKey lets a program call the API without holding a user's password.
//
// Only a SHA-256 digest of the key is stored. bcrypt would be the usual choice
// for a secret at rest, but this one is verified on every request and is 256
// bits of entropy from a CSPRNG rather than a human-chosen password, so a fast
// digest is the right trade: there is nothing to brute-force.
type APIKey struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"-"`
	Name         string     `json:"name"`
	Prefix       string     `json:"prefix"`
	Scopes       string     `json:"scopes"`
	RateLimit    int        `json:"rate_limit_per_minute"`
	ExpiresAt    *NaiveTime `json:"expires_at"`
	LastUsedAt   *NaiveTime `json:"last_used_at"`
	Revoked      bool       `json:"revoked"`
	CreatedAt    NaiveTime  `json:"created_at"`
	RequestsUsed int        `json:"-"`
}

// ScopeList splits the stored scopes for checking.
func (k *APIKey) ScopeList() []string {
	var out []string
	for _, s := range strings.Split(k.Scopes, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Allows reports whether the key carries a scope. "admin" implies everything.
func (k *APIKey) Allows(scope string) bool {
	for _, s := range k.ScopeList() {
		if s == scope || s == "admin" {
			return true
		}
	}
	return false
}

// Usable reports whether the key may authenticate right now.
func (k *APIKey) Usable() bool {
	if k.Revoked {
		return false
	}
	// Compare wall clocks, not instants. expires_at is stored as a naive UTC+7
	// reading, and the sqlite driver hands it back tagged UTC — so comparing the
	// two as instants made every key outlive its stated expiry by seven hours.
	// TimeLayout is zero-padded, so string order is chronological; PurgeJobs
	// already relies on the same property.
	if k.ExpiresAt != nil && !k.ExpiresAt.IsZero() &&
		NowVN().Format(TimeLayout) > k.ExpiresAt.Format(TimeLayout) {
		return false
	}
	return true
}

const apiKeyCols = `id,user_id,name,prefix,COALESCE(scopes,''),COALESCE(rate_limit_per_minute,60),
	expires_at,last_used_at,COALESCE(revoked,0),created_at`

func scanAPIKey(s interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	var expires, lastUsed, created sql.NullTime
	err := s.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.Scopes, &k.RateLimit,
		&expires, &lastUsed, &k.Revoked, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.ExpiresAt = naivePtr(expires)
	k.LastUsedAt = naivePtr(lastUsed)
	k.CreatedAt = naive(created)
	return &k, nil
}

func (db *DB) CreateAPIKey(k *APIKey, hash string) error {
	res, err := db.Exec(`INSERT INTO api_keys
		(user_id,name,prefix,key_hash,scopes,rate_limit_per_minute,expires_at,revoked,created_at)
		VALUES (?,?,?,?,?,?,?,0,?)`,
		k.UserID, k.Name, k.Prefix, hash, k.Scopes, k.RateLimit,
		naiveArg(k.ExpiresAt), NowVN().Format(TimeLayout))
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) APIKeysByUser(userID int64) ([]*APIKey, error) {
	rows, err := db.Query(`SELECT `+apiKeyCols+` FROM api_keys WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// APIKeyByHash is the authentication lookup. The digest is indexed, so this is
// one index hit per request.
func (db *DB) APIKeyByHash(hash string) (*APIKey, error) {
	return scanAPIKey(db.QueryRow(`SELECT `+apiKeyCols+` FROM api_keys WHERE key_hash=?`, hash))
}

func (db *DB) TouchAPIKey(id int64) error {
	_, err := db.Exec(`UPDATE api_keys SET last_used_at=? WHERE id=?`, NowVN().Format(TimeLayout), id)
	return err
}

func (db *DB) RevokeAPIKey(id, userID int64) error {
	res, err := db.Exec(`UPDATE api_keys SET revoked=1 WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteAPIKey(id, userID int64) error {
	res, err := db.Exec(`DELETE FROM api_keys WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ------------------------------------------------------------------ webhooks

type Webhook struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"-"`
	URL          string     `json:"url"`
	Events       string     `json:"events"`
	IsActive     bool       `json:"is_active"`
	LastStatus   *int       `json:"last_status"`
	LastCalledAt *NaiveTime `json:"last_called_at"`
	CreatedAt    NaiveTime  `json:"created_at"`
}

func (h *Webhook) Subscribes(event string) bool {
	for _, e := range strings.Split(h.Events, ",") {
		e = strings.TrimSpace(e)
		if e == event || e == "*" {
			return true
		}
	}
	return false
}

const webhookCols = `id,user_id,url,COALESCE(events,''),COALESCE(is_active,1),
	last_status,last_called_at,created_at`

func scanWebhook(s interface{ Scan(...any) error }) (*Webhook, error) {
	var h Webhook
	var called, created sql.NullTime
	err := s.Scan(&h.ID, &h.UserID, &h.URL, &h.Events, &h.IsActive, &h.LastStatus, &called, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	h.LastCalledAt = naivePtr(called)
	h.CreatedAt = naive(created)
	return &h, nil
}

func (db *DB) CreateWebhook(h *Webhook, secretEnc string) error {
	res, err := db.Exec(`INSERT INTO webhooks (user_id,url,secret_enc,events,is_active,created_at)
		VALUES (?,?,?,?,?,?)`,
		h.UserID, h.URL, secretEnc, h.Events, h.IsActive, NowVN().Format(TimeLayout))
	if err != nil {
		return err
	}
	h.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) WebhooksByUser(userID int64) ([]*Webhook, error) {
	rows, err := db.Query(`SELECT `+webhookCols+` FROM webhooks WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Webhook{}
	for rows.Next() {
		h, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ActiveWebhooks is the delivery path: it returns the encrypted secret too.
func (db *DB) ActiveWebhooks(userID int64) ([]*Webhook, []string, error) {
	rows, err := db.Query(`SELECT `+webhookCols+`,COALESCE(secret_enc,'')
		FROM webhooks WHERE user_id=? AND is_active=1`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var hooks []*Webhook
	var secrets []string
	for rows.Next() {
		var h Webhook
		var called, created sql.NullTime
		var secret string
		if err := rows.Scan(&h.ID, &h.UserID, &h.URL, &h.Events, &h.IsActive,
			&h.LastStatus, &called, &created, &secret); err != nil {
			return nil, nil, err
		}
		h.LastCalledAt = naivePtr(called)
		h.CreatedAt = naive(created)
		hooks = append(hooks, &h)
		secrets = append(secrets, secret)
	}
	return hooks, secrets, rows.Err()
}

func (db *DB) WebhookByID(id, userID int64) (*Webhook, error) {
	return scanWebhook(db.QueryRow(`SELECT `+webhookCols+` FROM webhooks WHERE id=? AND user_id=?`, id, userID))
}

func (db *DB) WebhookSecret(id int64) (string, error) {
	var enc string
	err := db.QueryRow(`SELECT COALESCE(secret_enc,'') FROM webhooks WHERE id=?`, id).Scan(&enc)
	return enc, err
}

func (db *DB) UpdateWebhook(h *Webhook) error {
	_, err := db.Exec(`UPDATE webhooks SET url=?,events=?,is_active=? WHERE id=? AND user_id=?`,
		h.URL, h.Events, h.IsActive, h.ID, h.UserID)
	return err
}

func (db *DB) RecordWebhookCall(id int64, status int) error {
	_, err := db.Exec(`UPDATE webhooks SET last_status=?, last_called_at=? WHERE id=?`,
		status, NowVN().Format(TimeLayout), id)
	return err
}

func (db *DB) DeleteWebhook(id, userID int64) error {
	res, err := db.Exec(`DELETE FROM webhooks WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AllTimes is a convenience for tests that need a stable clock reference.
var _ = time.Now
