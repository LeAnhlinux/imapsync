// Package security holds password hashing, JWTs and encryption of the IMAP
// credentials stored in the database.
//
// The formats are deliberately the ones the Python backend used — bcrypt
// hashes, HS256 tokens and Fernet tokens — so an existing database keeps
// working without a re-encryption pass.
package security

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/fernet/fernet-go"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Manager struct {
	secret []byte
	key    *fernet.Key
	ttl    time.Duration
}

func New(secretKey, fernetKey string, ttl time.Duration) (*Manager, error) {
	k, err := fernet.DecodeKey(fernetKey)
	if err != nil {
		return nil, fmt.Errorf("IMAPSYNC_FERNET_KEY is not a valid Fernet key: %w", err)
	}
	return &Manager{secret: []byte(secretKey), key: k, ttl: ttl}, nil
}

func HashPassword(plain string) (string, error) {
	// Cost 12, matching what the Python side produced.
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	return string(b), err
}

func VerifyPassword(plain, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

func (m *Manager) CreateToken(userID int64) (string, error) {
	claims := jwt.MapClaims{
		"sub": strconv.FormatInt(userID, 10),
		"exp": time.Now().Add(m.ttl).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

var ErrInvalidToken = errors.New("invalid token")

func (m *Manager) UserIDFromToken(tokenStr string) (int64, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})
	if err != nil || !token.Valid {
		return 0, ErrInvalidToken
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, ErrInvalidToken
	}
	sub, _ := claims["sub"].(string)
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return 0, ErrInvalidToken
	}
	return id, nil
}

func (m *Manager) Encrypt(plain string) (string, error) {
	tok, err := fernet.EncryptAndSign([]byte(plain), m.key)
	return string(tok), err
}

func (m *Manager) Decrypt(token string) (string, error) {
	// A zero TTL disables the freshness check: stored credentials are old by
	// definition, and a TTL here would reject every one of them.
	out := fernet.VerifyAndDecrypt([]byte(token), 0, []*fernet.Key{m.key})
	if out == nil {
		return "", errors.New("could not decrypt: wrong IMAPSYNC_FERNET_KEY or corrupt value")
	}
	return string(out), nil
}
