package services

import "testing"

// Captured verbatim from imapsync --justfoldersizes against a real IMAP server.
// The parser was previously written against a guessed format and matched none
// of this, so the folder browser always returned an empty list.
const realOutput = `
Host1: folder [INBOX] has 1 messages mentioned by SELECT
++++ Calculating sizes of folders
Host1 folder     1/5 [INBOX]                             Size:     19108 Messages:     1 Biggest:     19108
Host2 folder     1/5 [INBOX]                             Size:     19108 Messages:     1 Biggest:     19108
Host2-Host1                                                            0               0                  0

Host1 folder     2/5 [Drafts]                            Size:         0 Messages:     0 Biggest:         0
Host2 folder     2/5 [Drafts]                            Size:         0 Messages:     0 Biggest:         0

Host1 folder     4/5 [Sent Mail]                         Size:       412 Messages:     3 Biggest:       200
Host2 folder     4/5 [Sent Mail]                         Size:       412 Messages:     3 Biggest:       200

Host1 Nb folders:                     5 folders
Host1 Total size:                 19108 bytes (18.660 KiB)
`

func TestParseFolderSizes(t *testing.T) {
	got := parseFolderSizes(realOutput)
	if len(got) != 3 {
		t.Fatalf("parsed %d folders, want 3: %+v", len(got), got)
	}

	want := []struct {
		name     string
		messages int
		size     int64
	}{
		{"INBOX", 1, 19108},
		{"Drafts", 0, 0},
		// A name with a space must survive intact.
		{"Sent Mail", 3, 412},
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Messages != w.messages || got[i].Size != w.size {
			t.Errorf("folder %d = %+v, want {%s %d %d}", i, got[i], w.name, w.messages, w.size)
		}
	}
}

func TestParseFolderSizesIgnoresHost2(t *testing.T) {
	// Host2 is the same mailbox passed twice; counting it would double every
	// folder in the list.
	for _, f := range parseFolderSizes(realOutput) {
		if f.Name == "" {
			t.Error("empty folder name parsed")
		}
	}
	if n := len(parseFolderSizes("Host2 folder 1/1 [X] Size: 5 Messages: 1 Biggest: 5")); n != 0 {
		t.Errorf("Host2-only output produced %d folders, want 0", n)
	}
}
