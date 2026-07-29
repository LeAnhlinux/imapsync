package api

import (
	"strings"
	"testing"

	"github.com/imapsync/web-backend/internal/store"
)

func TestGenerateKey(t *testing.T) {
	key, prefix, hash, err := generateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, KeyPrefix) {
		t.Errorf("key %q lacks the %q prefix", key, KeyPrefix)
	}
	if len(key) < 40 {
		t.Errorf("key is only %d chars", len(key))
	}
	if !strings.HasPrefix(key, prefix) {
		t.Errorf("prefix %q is not a prefix of the key", prefix)
	}
	if hash != hashKey(key) {
		t.Error("stored hash does not match hashKey of the key")
	}
	if strings.Contains(hash, key) {
		t.Error("the hash leaks the key")
	}

	other, _, _, _ := generateKey()
	if other == key {
		t.Error("two generated keys collided")
	}
}

func TestScopes(t *testing.T) {
	k := &store.APIKey{Scopes: "sync:read,sync:write"}
	for _, want := range []string{"sync:read", "sync:write"} {
		if !k.Allows(want) {
			t.Errorf("key should allow %s", want)
		}
	}
	for _, deny := range []string{"admin", "batch:write", "profiles:write"} {
		if k.Allows(deny) {
			t.Errorf("key should not allow %s", deny)
		}
	}

	admin := &store.APIKey{Scopes: "admin"}
	for _, want := range ValidScopes {
		if !admin.Allows(want) {
			t.Errorf("admin should imply %s", want)
		}
	}
}

func TestKeyUsable(t *testing.T) {
	if (&store.APIKey{Revoked: true}).Usable() {
		t.Error("a revoked key must not be usable")
	}
	past := store.NaiveTime{Time: store.NowVN().AddDate(0, 0, -1)}
	if (&store.APIKey{ExpiresAt: &past}).Usable() {
		t.Error("an expired key must not be usable")
	}
	future := store.NaiveTime{Time: store.NowVN().AddDate(0, 0, 1)}
	if !(&store.APIKey{ExpiresAt: &future}).Usable() {
		t.Error("a key expiring tomorrow must still be usable")
	}
	if !(&store.APIKey{}).Usable() {
		t.Error("a key with no expiry must be usable")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := &rateLimiter{windows: map[int64]*window{}}
	for i := 1; i <= 3; i++ {
		remaining, _, ok := rl.take(1, 3)
		if !ok {
			t.Fatalf("request %d rejected early", i)
		}
		if remaining != 3-i {
			t.Errorf("request %d: remaining %d, want %d", i, remaining, 3-i)
		}
	}
	if _, _, ok := rl.take(1, 3); ok {
		t.Error("fourth request should have been rejected")
	}
	// A different key has its own window.
	if _, _, ok := rl.take(2, 3); !ok {
		t.Error("a second key should not share the first key's window")
	}
}
