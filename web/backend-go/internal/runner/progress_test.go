package runner

import "testing"

// Every line below was captured verbatim from imapsync 2.229 on the server:
// the copy lines from a real 7-message transfer between test1.lamiral.info and
// test2.lamiral.info, the sizing and ETA lines from a 9,802-line migration log.
//
// This file exists because the previous progress regex was written by guessing
// at the output rather than reading it. It looked for "Syncing folder [x] (n/m)",
// which imapsync does not print, so progress sat at 0% for every job ever run
// and no Telegram milestone ever fired.

const (
	copyLine1 = `msg Borrador/1 {1050}            copied to claudeprobe/1          0.26 msgs/s  0.271 KiB/s 1.025 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:40 +0700 +07  23 s  6/7 msgs left`
	copyLine6 = `msg Borrador/6 {11379}           copied to claudeprobe/6          0.73 msgs/s  1.853 KiB/s 15.299 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:23 +0700 +07  1 s  1/7 msgs left`
	etaLine   = `ETA: Thursday 30 July 2026-07-30 09:20:14 +0700 +07  0 s  7/7 msgs left`
	sizeLine  = `Host1 folder    4/15 [Deleted Items]                     Size:      2239 Messages:     3 Biggest:       750`
)

func TestScanCopyLineCountsMessagesAndBytes(t *testing.T) {
	var p Progress
	if !scanLine(copyLine1, &p) {
		t.Fatal("a copy line reported no change")
	}
	if p.Messages != 1 || p.Bytes != 1050 {
		t.Errorf("after one copy: %d msgs, %d bytes; want 1 and 1050", p.Messages, p.Bytes)
	}
	scanLine(copyLine6, &p)
	if p.Messages != 2 || p.Bytes != 1050+11379 {
		t.Errorf("after two copies: %d msgs, %d bytes; want 2 and 12429", p.Messages, p.Bytes)
	}
}

func TestScanCopyLineTracksPercentAndETA(t *testing.T) {
	var p Progress
	scanLine(copyLine1, &p)
	// "6/7 msgs left" means one of seven is done.
	if want := 14.3; p.Percent != want {
		t.Errorf("percent is %v, want %v", p.Percent, want)
	}
	if p.ETA != "23s" {
		t.Errorf("ETA is %q, want \"23s\"", p.ETA)
	}
	if p.Folder != "1/7 messages" {
		t.Errorf("label is %q, want \"1/7 messages\"", p.Folder)
	}
}

func TestScanWholeTransferSumsToFolderSize(t *testing.T) {
	// The seven sizes are the ones imapsync reported for the folder, and
	// --justfoldersizes gave that folder as "Size: 16664". Summing the braces
	// has to reproduce it exactly, or the byte column is decorative.
	lines := []string{
		`msg Borrador/1 {1050}            copied to claudeprobe/1          0.26 msgs/s  0.271 KiB/s 1.025 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:40 +0700 +07  23 s  6/7 msgs left`,
		`msg Borrador/2 {847}             copied to claudeprobe/2          0.43 msgs/s  0.396 KiB/s 1.853 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:30 +0700 +07  12 s  5/7 msgs left`,
		`msg Borrador/3 {798}             copied to claudeprobe/3          0.54 msgs/s  0.473 KiB/s 2.632 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:26 +0700 +07  7 s  4/7 msgs left`,
		`msg Borrador/4 {794}             copied to claudeprobe/4          0.62 msgs/s  0.528 KiB/s 3.407 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:25 +0700 +07  5 s  3/7 msgs left`,
		`msg Borrador/5 {798}             copied to claudeprobe/5          0.68 msgs/s  0.570 KiB/s 4.187 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:24 +0700 +07  3 s  2/7 msgs left`,
		`msg Borrador/6 {11379}           copied to claudeprobe/6          0.73 msgs/s  1.853 KiB/s 15.299 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:23 +0700 +07  1 s  1/7 msgs left`,
		`msg Borrador/7 {998}             copied to claudeprobe/7          0.76 msgs/s  1.758 KiB/s 16.273 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:23 +0700 +07  0 s  0/7 msgs left`,
	}
	var p Progress
	for _, l := range lines {
		scanLine(l, &p)
	}
	if p.Messages != 7 || p.Bytes != 16664 {
		t.Errorf("got %d msgs / %d bytes, want 7 / 16664", p.Messages, p.Bytes)
	}
	if p.Percent != 100 {
		t.Errorf("a finished transfer sits at %v%%, want 100", p.Percent)
	}
}

func TestScanSizingPhase(t *testing.T) {
	// Sizing runs for minutes before the first message moves — 264 seconds on a
	// 9,000-message Gmail account. Without this the UI shows nothing at all for
	// the part of the run users are most likely to be staring at.
	var p Progress
	if !scanLine(sizeLine, &p) {
		t.Fatal("a folder sizing line reported no change")
	}
	if p.Folder != "sizing 4/15" {
		t.Errorf("label is %q, want \"sizing 4/15\"", p.Folder)
	}
	if p.Messages != 0 || p.Percent != 0 {
		t.Error("sizing counted as transferred work")
	}
}

func TestScanStandaloneETALine(t *testing.T) {
	var p Progress
	scanLine(etaLine, &p)
	if p.Percent != 0 {
		t.Errorf("nothing copied yet but percent is %v", p.Percent)
	}
	if p.Folder != "0/7 messages" {
		t.Errorf("label is %q, want \"0/7 messages\"", p.Folder)
	}
}

func TestScanIgnoresOrdinaryOutput(t *testing.T) {
	// Header and summary lines must not be mistaken for progress. The summary in
	// particular contains numbers that look inviting.
	for _, line := range []string{
		"Here is imapsync 2.229 on host sync, a linux system with 3.1/3.8 free GiB of RAM",
		"++++ Listing folders",
		"Folders sizes before the synchronization. It can take some time. Be patient.",
		"Messages transferred                    : 0 (could be 18945 without dry mode)",
		"Total bytes transferred                 : 0 (0.000 KiB)",
		"Detected 0 errors",
		"",
	} {
		var p Progress
		if scanLine(line, &p) {
			t.Errorf("line was read as progress: %q -> %+v", line, p)
		}
	}
}

func TestScanSurvivesFolderNamesWithSpacesAndUnicode(t *testing.T) {
	// Real accounts have folders like "[Gmail]/Thư đã gửi". The pattern must not
	// assume the folder is one unbroken word.
	line := `msg [Gmail]/Thư đã gửi/42 {2048}      copied to INBOX.Sent/7          1.00 msgs/s  2.0 KiB/s 2.0 KiB copied ETA: Thursday 30 July 2026-07-30 09:20:23 +0700 +07  9 s  3/10 msgs left`
	var p Progress
	if !scanLine(line, &p) {
		t.Fatal("a copy line with a spaced unicode folder was not recognised")
	}
	if p.Messages != 1 || p.Bytes != 2048 || p.Percent != 70 {
		t.Errorf("got %+v, want 1 msg / 2048 bytes / 70%%", p)
	}
}
