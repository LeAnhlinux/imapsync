package runner

import (
	"strings"
	"testing"
)

// Captured from job #7 on the production server: a 14,400-message Gmail to
// Tino migration. The tail of this log is what the old parser used as its
// summary, which is why the Telegram notification quoted a homepage URL and a
// pid file being removed.
const realStats = `Host2 Total size:             124285135 bytes (118.528 MiB)
++++ Statistics
Transfer started on                     : Thursday 30 July 2026-07-30 09:45:23 +0700 +07
Transfer time                           : 8566.7 sec
Folders synced                          : 15/15 synced
Messages transferred                    : 14400 
Messages skipped                        : 4547
Messages found in host1 not in host2    : 3 messages
Total bytes transferred                 : 1649282082 (1.536 GiB)
Detected 0 errors
Check if a new imapsync release is available by adding --releasecheck
Homepage: https://imapsync.lamiral.info/
Exiting with return value 0 (EX_OK: successful termination) 0/50 nb_errors/max_errors PID 22547
Removing pidfile /opt/imapsync-web/data/tmp/imapsync_7.pid
No log file because of option --nolog`

func TestSummaryReadsTheStatisticsNotTheTail(t *testing.T) {
	s := parseSummary(realStats, 0, false)

	if s.MessagesTransferred != 14400 || s.BytesTransferred != 1649282082 {
		t.Errorf("counts are %d / %d, want 14400 / 1649282082",
			s.MessagesTransferred, s.BytesTransferred)
	}

	for _, junk := range []string{
		"Homepage", "releasecheck", "pidfile", "return value", "--nolog", "PID",
	} {
		if strings.Contains(s.Text, junk) {
			t.Errorf("the summary still carries housekeeping output: %q\n%s", junk, s.Text)
		}
	}
	for _, want := range []string{
		"15/15 folders",
		"4,547 already there",
		"3 still only on the source",
		"2h22m47s",
	} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("the summary is missing %q; got:\n%s", want, s.Text)
		}
	}
}

func TestSummaryExplainsAFailure(t *testing.T) {
	// Exit 161 is the one that actually happens: a wrong password on the source.
	// The old summary showed the last five lines, which never say that.
	log := `Failure: Error login on [imap.gmail.com] with user [me] : AUTHENTICATIONFAILED
Detected 1 errors
Exiting with return value 161 (EXIT_AUTHENTICATION_FAILURE_USER1)`
	s := parseSummary(log, 161, false)

	if !strings.Contains(s.Text, "Wrong username or password for the source account") {
		t.Errorf("the failure is not explained; got:\n%s", s.Text)
	}
	if !strings.Contains(s.Text, "AUTHENTICATIONFAILED") {
		t.Errorf("imapsync's own diagnostic was dropped; got:\n%s", s.Text)
	}
	if s.ErrorsCount != 1 {
		t.Errorf("error count is %d, want 1", s.ErrorsCount)
	}
}

func TestSummaryOfAnAbortedRun(t *testing.T) {
	s := parseSummary("Messages transferred : 12\nFolders synced : 2/15 synced", 143, true)
	if !s.Aborted {
		t.Error("the aborted flag was lost")
	}
	if !strings.Contains(s.Text, "Stopped before it finished") {
		t.Errorf("an aborted run does not say so; got:\n%s", s.Text)
	}
	if strings.Contains(s.Text, "exited with code") {
		t.Errorf("a deliberate stop is reported as a crash; got:\n%s", s.Text)
	}
}

func TestSummaryStaysQuietWhenNothingHappened(t *testing.T) {
	// A clean run with nothing to do should not manufacture prose.
	s := parseSummary("Messages transferred : 0\nMessages skipped : 0\nDetected 0 errors", 0, false)
	if strings.Contains(s.Text, "already there") || strings.Contains(s.Text, "still only") {
		t.Errorf("zero counts were rendered as facts; got:\n%s", s.Text)
	}
}

func TestExitReasonNamesTheUsualFailures(t *testing.T) {
	for code, want := range map[int]string{
		161: "source",
		162: "destination",
		113: "quota",
		12:  "TLS",
		0:   "",
	} {
		got := exitReason(code)
		if want == "" {
			if got != "" {
				t.Errorf("exit 0 produced %q", got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("exit %d reads %q, expected it to mention %q", code, got, want)
		}
	}
	if !strings.Contains(exitReason(77), "77") {
		t.Error("an unknown code should still report its number")
	}
}

func TestComma(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0", 999: "999", 1000: "1,000", 14400: "14,400",
		1649282082: "1,649,282,082", -14400: "-14,400",
	} {
		if got := Comma(in); got != want {
			t.Errorf("Comma(%d) = %q, want %q", in, got, want)
		}
	}
}
