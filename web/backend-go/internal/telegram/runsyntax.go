package telegram

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/imapsync/web-backend/internal/services"
)

// Parsing for the /run command's mailbox specification.
//
// Six semicolon-separated fields in a row read fine to a machine and badly to
// a person: nothing in `a;b;c;d;e;f` says where the source ends and the
// destination begins, and getting that backwards copies the wrong way. A space
// splits the two sides, semicolons split the fields inside one side:
//
//	/run mail.old.com;me@old.com;password mail.new.com;me@new.com;password
//
// A password containing a space has nothing else to split on, so the labelled
// form stays available for that case.

var (
	fromLabels = map[string]bool{
		"from": true, "source": true, "src": true, "old": true,
		"cu": true, "cũ": true, "nguon": true, "nguồn": true, "1": true,
	}
	toLabels = map[string]bool{
		"to": true, "dest": true, "destination": true, "target": true, "new": true,
		"moi": true, "mới": true, "dich": true, "đích": true, "2": true,
	}
	optionLabels = map[string]bool{
		"options": true, "option": true, "opts": true, "flags": true, "tuychon": true,
	}
	// A label is a bare word before a colon. Excluding dots keeps host:port and
	// bare hostnames from looking like labels.
	labelLine = regexp.MustCompile(`^\s*([^\s:;.]+)\s*:`)
)

const runUsage = `Usage:

/run mail.old.com;me@old.com;password mail.new.com;me@new.com;password

A space separates the two mailboxes; each one is host;user;password.
The left one is copied to the right one.

Options go at the end:
/run old.com;me;pw new.com;me;pw --dry --maxage 30 --nossl2

Paste one such line per mailbox to queue several as a batch.

If a password contains a space, use the long form instead:
/run
from: mail.old.com;me@old.com;my password
to: mail.new.com;me@new.com;password`

type syntaxError struct{ msg string }

func (e syntaxError) Error() string { return e.msg }

func errf(format string, args ...any) error {
	return syntaxError{fmt.Sprintf(format, args...)}
}

func splitSide(what, value string) (host, user, password string, err error) {
	parts := strings.Split(value, ";")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		// Never quote the input back. This message is a new message from the
		// bot, and the bot cannot delete its own messages the way it deletes
		// the user's -- so echoing the line would leave the password in the
		// chat permanently, immediately after telling the user it was removed.
		// A wrong field count is usually a password containing ';', which is
		// exactly when the echo would have leaked it.
		return "", "", "", errf(
			"The %s mailbox needs exactly host;user;password — I counted %d field(s), not 3.\n"+
				"If the password contains a ';' or a space, that is the cause.\n\n%s",
			what, len(parts), runUsage)
	}
	return parts[0], parts[1], parts[2], nil
}

func isLabeled(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" && labelLine.MatchString(line) {
			return true
		}
	}
	return false
}

func parseLabeled(text string) (services.Row, error) {
	var row services.Row
	var haveFrom, haveTo bool

	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := labelLine.FindStringSubmatch(line)
		if m == nil {
			// Line numbers, not the line: an unparsed line still holds whatever
			// the user typed, password included.
			return row, errf("I do not understand line %d. Each line needs a "+
				"\"from:\" or \"to:\" label.\n\n%s", lineNo, runUsage)
		}
		key := strings.ToLower(m[1])
		value := strings.TrimSpace(line[len(m[0]):])

		switch {
		case fromLabels[key]:
			h, u, p, err := splitSide("'from'", value)
			if err != nil {
				return row, err
			}
			row.Host1, row.User1, row.Password1 = h, u, p
			haveFrom = true
		case toLabels[key]:
			h, u, p, err := splitSide("'to'", value)
			if err != nil {
				return row, err
			}
			row.Host2, row.User2, row.Password2 = h, u, p
			haveTo = true
		case optionLabels[key]:
			row.ExtraOptions = services.ParseOptionString(value)
		default:
			return row, errf("Unknown label %q.\n\n%s", m[1], runUsage)
		}
	}

	switch {
	case !haveFrom && !haveTo:
		return row, syntaxError{runUsage}
	case !haveFrom:
		return row, errf("Missing the 'from' line — I do not know what to copy.\n\n%s", runUsage)
	case !haveTo:
		return row, errf("Missing the 'to' line — I do not know where to copy it.\n\n%s", runUsage)
	}
	return row, nil
}

func parseLine(line string) (services.Row, error) {
	var row services.Row
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return row, syntaxError{runUsage}
	}

	// Options run from the first --flag to the end of the line, so a flag that
	// takes a value ("--maxage 30") keeps its value.
	optStart := len(tokens)
	for i, t := range tokens {
		if strings.HasPrefix(t, "--") {
			optStart = i
			break
		}
	}
	sides := tokens[:optStart]
	row.ExtraOptions = services.ParseOptionString(strings.Join(tokens[optStart:], " "))

	switch len(sides) {
	case 2:
		h, u, p, err := splitSide("source", sides[0])
		if err != nil {
			return row, err
		}
		row.Host1, row.User1, row.Password1 = h, u, p
		h, u, p, err = splitSide("destination", sides[1])
		if err != nil {
			return row, err
		}
		row.Host2, row.User2, row.Password2 = h, u, p
	case 1:
		// The flat CSV row, so a line copied out of a batch file still works.
		fields := strings.Split(sides[0], ";")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) < 6 {
			return row, errf("I need two mailboxes separated by a space.\n\n%s", runUsage)
		}
		for _, f := range fields[:6] {
			if f == "" {
				return row, errf("I need two mailboxes separated by a space.\n\n%s", runUsage)
			}
		}
		row.Host1, row.User1, row.Password1 = fields[0], fields[1], fields[2]
		row.Host2, row.User2, row.Password2 = fields[3], fields[4], fields[5]
		if len(fields) > 6 && fields[6] != "" {
			for k, v := range services.ParseOptionString(fields[6]) {
				if _, taken := row.ExtraOptions[k]; !taken {
					row.ExtraOptions[k] = v
				}
			}
		}
	default:
		return row, errf(
			"I found %d mailboxes on that line, I need exactly 2. "+
				"A password with a space in it needs the long form.\n\n%s", len(sides), runUsage)
	}
	return row, nil
}

// parseRun reads a whole /run body: a labelled block, or one mailbox pair per
// line.
func parseRun(text string) ([]services.Row, error) {
	if isLabeled(text) {
		row, err := parseLabeled(text)
		if err != nil {
			return nil, err
		}
		return []services.Row{row}, nil
	}

	var rows []services.Row
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		row, err := parseLine(line)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, syntaxError{runUsage}
	}
	return rows, nil
}

// describeRow spells out the direction so a mistake is obvious before it copies.
func describeRow(r services.Row, dryRun bool) string {
	lines := []string{
		fmt.Sprintf("  FROM  %s  on %s", r.User1, r.Host1),
		fmt.Sprintf("  TO    %s  on %s", r.User2, r.Host2),
	}
	if dryRun {
		lines = append(lines, "  MODE  dry run, nothing will be written")
	}
	return strings.Join(lines, "\n")
}
