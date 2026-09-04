package services

import (
	"strings"
	"testing"
)

func TestParseCSVRefusesRatherThanDropRows(t *testing.T) {
	// Malformed rows used to be skipped silently. A 200-mailbox migration file
	// with three bad lines produced 197 rows, no error, and nothing saying
	// which three — so those mailboxes were simply never copied.
	_, err := ParseCSV(strings.Join([]string{
		"a.com;u1;p1;b.com;u2;p2",
		"bad;row",
		"c.com;u3;p3;d.com;u4;p4",
	}, "\n"))
	if err == nil {
		t.Fatal("a malformed line was accepted and dropped")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error does not say which line: %v", err)
	}

	// An empty field is just as bad: it would sync against a blank password.
	if _, err := ParseCSV("a.com;u1;;b.com;u2;p2"); err == nil {
		t.Error("a row with an empty password was accepted")
	}
}

func TestParseCSVReadsAWellFormedFile(t *testing.T) {
	rows, err := ParseCSV(strings.Join([]string{
		"# host1;user1;password1;host2;user2;password2",
		"a.com;u1;p1;b.com;u2;p2",
		"",
		"c.com;u3;p3;d.com;u4;p4;--maxage 7",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(rows))
	}
	if rows[0].Host1 != "a.com" || rows[0].Password1 != "p1" {
		t.Errorf("first row is %+v", rows[0])
	}
	if rows[1].ExtraOptions["maxage"] == nil {
		t.Errorf("the options column was dropped: %+v", rows[1].ExtraOptions)
	}
}

func TestParseCSVKeepsPasswordsExactly(t *testing.T) {
	// Every field used to be trimmed, passwords included. A password may
	// legitimately begin or end with a space, and trimming one produces an
	// authentication failure that reads as the server rejecting a correct
	// password — with no way to tell from the log that the file was fine.
	rows, err := ParseCSV("a.com; u1 ; lead and trail ;b.com;u2;p@ss w0rd!")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(rows))
	}
	if rows[0].Password1 != " lead and trail " {
		t.Errorf("the source password came back as %q; the spaces were stripped", rows[0].Password1)
	}
	if rows[0].Password2 != "p@ss w0rd!" {
		t.Errorf("the destination password came back as %q", rows[0].Password2)
	}
	// Hosts and users, where surrounding space is certainly a typo, are trimmed.
	if rows[0].User1 != "u1" {
		t.Errorf("the username kept its padding: %q", rows[0].User1)
	}
}

func TestParseCSVIgnoresCommentsAndBlanks(t *testing.T) {
	rows, err := ParseCSV("\n# header\n\n   \na.com;u;p;b.com;u;p\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("parsed %d rows, want 1", len(rows))
	}
}

func TestDescribeBatchNeverPrintsAPassword(t *testing.T) {
	// This text goes into a Telegram message, which the bot cannot delete.
	rows, err := ParseCSV("a.com;alice@a.com;TOP-SECRET-1;b.com;alice@b.com;TOP-SECRET-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		summary := r.Host1 + r.User1 + r.Host2 + r.User2
		for _, secret := range []string{"TOP-SECRET-1", "TOP-SECRET-2"} {
			if strings.Contains(summary, secret) {
				t.Errorf("a password is reachable from the non-secret fields: %q", summary)
			}
		}
	}
}
