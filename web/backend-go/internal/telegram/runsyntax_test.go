package telegram

import (
	"strings"
	"testing"
)

func TestParseRun(t *testing.T) {
	cases := []struct{ name, in, wantFrom, wantErr string }{
		{"short form", "mail.cu.com;me@cu.com;pw1 mail.moi.com;me@moi.com;pw2", "me@cu.com", ""},
		{"with options", "a.com;u1;p1 b.com;u2;p2 --dry --maxage 30", "u1", ""},
		{"flat legacy", "a.com;u1;p1;b.com;u2;p2", "u1", ""},
		{"labeled", "from: a.com;u1;mat khau co dau cach\nto: b.com;u2;p2", "u1", ""},
		{"vietnamese labels", "cu: a.com;u1;p1\nmoi: b.com;u2;p2", "u1", ""},
		{"one side only", "a.com;u1;p1", "", "two mailboxes separated by a space"},
		{"missing field", "a.com;u1 b.com;u2;p2", "", "exactly host;user;password"},
		{"space in password", "a.com;u1;mat khau b.com;u2;p2", "", "I found 3 mailboxes"},
		{"typo label", "fromm: a.com;u1;p1", "", "Unknown label"},
		{"missing to", "from: a.com;u1;p1", "", "Missing the 'to' line"},
	}
	for _, c := range cases {
		rows, err := parseRun(c.in)
		if c.wantErr != "" {
			if err == nil || !contains(err.Error(), c.wantErr) {
				t.Errorf("%s: want error containing %q, got %v", c.name, c.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if rows[0].User1 != c.wantFrom {
			t.Errorf("%s: User1 = %q, want %q", c.name, rows[0].User1, c.wantFrom)
		}
	}
}

func TestParseRunMultiline(t *testing.T) {
	rows, err := parseRun("a.com;u1;p1 b.com;u2;p2\na.com;u3;p3 b.com;u4;p4")
	if err != nil || len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%v)", len(rows), err)
	}
	if rows[1].User1 != "u3" {
		t.Errorf("second row User1 = %q", rows[1].User1)
	}
}

func TestOptionsAndFlags(t *testing.T) {
	rows, _ := parseRun("a.com;u1;p1 b.com;u2;p2 --dry --nossl2 --maxage 30")
	e := rows[0].ExtraOptions
	if e["dry"] != true || e["nossl2"] != true || e["maxage"] != "30" {
		t.Errorf("options = %v", e)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A syntax error reply is a message from the bot. The bot deletes the user's
// message but cannot delete its own, so anything quoted back into an error
// stays in the chat for good -- right after the bot has said it removed the
// credentials. Both of these inputs used to echo the password verbatim.
func TestRunErrorsNeverQuoteTheInput(t *testing.T) {
	const pw = "S3cr3t-do-not-print"

	for _, in := range []string{
		// A password containing ';' -- the most likely way to get here.
		"a.com;me@a.com;" + pw + ";extra b.com;me@b.com;pw2",
		"from: a.com;me@a.com;" + pw + ";x\nto: b.com;me@b.com;pw2",
		// A mislabelled line: the label regex fails and the whole line was echoed.
		"from a.com;me@a.com;" + pw + "\nto: b.com;me@b.com;pw2",
		// Too few fields.
		"a.com;" + pw + " b.com;me@b.com;pw2",
		// Junk that still carries a credential.
		"garbage " + pw,
	} {
		_, err := parseRun(in)
		if err == nil {
			continue // parsed fine; nothing to leak
		}
		if strings.Contains(err.Error(), pw) {
			t.Errorf("the reply quotes the password.\n input: %q\n reply: %s", in, err.Error())
		}
	}
}

func TestRunErrorsStillSayWhatIsWrong(t *testing.T) {
	// Removing the echo must not reduce the message to "invalid input".
	_, err := parseRun("a.com;me@a.com;p;a;s;s b.com;me@b.com;pw2")
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	msg := err.Error()
	for _, want := range []string{"source", "field", "';'"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the reply no longer mentions %q; got:\n%s", want, msg)
		}
	}
}
