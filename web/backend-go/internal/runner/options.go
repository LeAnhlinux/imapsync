package runner

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/robfig/cron/v3"
)

// Allowlist for imapsync options coming from user input.
//
// extra_options is user-controlled (profile form, 7th CSV column, /run) and is
// spliced straight into imapsync's argv. imapsync exposes options that run
// shell commands (--pipemess) or eval Perl (--regexmess, --regextrans2), so
// anything not listed here is dropped rather than passed through. Options the
// runner manages itself — host, user, password, ssl, tmpdir, pidfile — are
// absent on purpose: letting them through would redirect credentials or
// overwrite files.
const (
	kindFlag = "flag"
	kindInt  = "int"
	kindStr  = "str"
)

var allowed = map[string]string{
	// behaviour toggles
	"automap": kindFlag, "usecache": kindFlag, "nousecache": kindFlag,
	"useuid": kindFlag, "nouseuid": kindFlag,
	"subscribe": kindFlag, "subscribeall": kindFlag, "nosubscribe": kindFlag,
	"syncinternaldates": kindFlag, "nosyncinternaldates": kindFlag,
	"idatefromheader": kindFlag, "syncacls": kindFlag, "addheader": kindFlag,
	"allowsizemismatch": kindFlag, "skipcrossduplicates": kindFlag,
	"skipemptyfolders": kindFlag, "checkfoldersexist": kindFlag,
	"nocheckfoldersexist": kindFlag, "checkmessageexists": kindFlag,
	"foldersizes": kindFlag, "nofoldersizes": kindFlag,
	"foldersizesatend": kindFlag, "nofoldersizesatend": kindFlag,
	// destructive, but legitimate migration features
	"delete1": kindFlag, "delete2": kindFlag, "delete2duplicates": kindFlag,
	"expunge1": kindFlag, "expunge2": kindFlag, "noexpunge1": kindFlag, "noexpunge2": kindFlag,
	// "just do X and stop" modes
	"justfolders": kindFlag, "justfoldersizes": kindFlag,
	"justconnect": kindFlag, "justlogin": kindFlag, "justbanner": kindFlag,
	// server presets
	"gmail1": kindFlag, "gmail2": kindFlag, "office1": kindFlag, "office2": kindFlag,
	"exchange1": kindFlag, "exchange2": kindFlag, "domino1": kindFlag, "domino2": kindFlag,
	// The fork's own additions -- parallel, pipeline, batchsize, condstore --
	// are deliberately NOT here. A code review traced what they actually do:
	//
	//   parallel  re-executes imapsync per worker and builds the child's argv by
	//             hand, including "--password1 <plaintext>" (imapsync:3315). That
	//             puts both mailbox passwords in ps output for the whole run,
	//             which is the exact thing writePassfile exists to prevent. The
	//             child also drops maxage, exclude, port and automap, and Abort
	//             cannot reach the workers.
	//   pipeline  prefetches with message_string() while the normal path uses
	//             message_to_file() (imapsync:2956 vs :12291). Nobody has shown
	//             the two return identical bytes, and that branch has no test, so
	//             it may append a different message body than a plain run.
	//   batchsize changes which IMAP fetches are issued; see sub batch_parse_headers.
	//   condstore is dead code -- condstore_changed_msgs is written and never read.
	//
	// They remain usable from the command line by whoever runs imapsync directly.
	// They are not offered to a web request until each is fixed and tested.
	// numeric limits
	"maxage": kindInt, "minage": kindInt, "maxsize": kindInt, "minsize": kindInt,
	"maxmessagespersecond": kindInt, "maxbytespersecond": kindInt,
	"timeout": kindInt, "timeout1": kindInt, "timeout2": kindInt,
	"port1": kindInt, "port2": kindInt,
	// string values
	"folder": kindStr, "include": kindStr, "exclude": kindStr,
	"prefix1": kindStr, "prefix2": kindStr, "sep1": kindStr, "sep2": kindStr,
	"authmech1": kindStr, "authmech2": kindStr, "useheader": kindStr,
}

var (
	unsafeValue = regexp.MustCompile(`[\x00-\x1f\x7f]`)
	// Perl regex code embedding, blocked by default in interpolation but not
	// worth the bet.
	regexCode = regexp.MustCompile(`\(\?\??\{`)
	digits    = regexp.MustCompile(`^\d+$`)
	// imapsync declares these as "=f", so a fraction is legal and sometimes
	// required: --maxmessagespersecond 0.5 is the value Gmail actually wants,
	// and requiring an integer rejected it.
	decimal = regexp.MustCompile(`^\d+(\.\d+)?$`)
)

// SanitizeOptions filters user-supplied imapsync options, returning the
// accepted ones and the names of everything dropped.
func SanitizeOptions(extra map[string]any) (map[string]any, []string) {
	accepted := map[string]any{}
	var rejected []string
	if extra == nil {
		return accepted, nil
	}

	for rawKey, value := range extra {
		key := strings.TrimLeft(strings.TrimSpace(rawKey), "-")
		kind, ok := allowed[key]
		if !ok {
			rejected = append(rejected, key)
			continue
		}

		if kind == kindFlag {
			if on, valid := truthy(value); valid {
				if on {
					accepted[key] = true
				}
			} else {
				rejected = append(rejected, key)
			}
			continue
		}

		text, ok := scalarString(value)
		if !ok {
			rejected = append(rejected, key)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || len(text) > 200 || unsafeValue.MatchString(text) || strings.HasPrefix(text, "-") {
			rejected = append(rejected, key)
			continue
		}
		if kind == kindInt {
			pat := digits
			if floatOptions[key] {
				pat = decimal
			}
			if !pat.MatchString(text) {
				rejected = append(rejected, key)
				continue
			}
		}
		if kind == kindStr && regexCode.MatchString(text) {
			rejected = append(rejected, key)
			continue
		}
		accepted[key] = text
	}

	sort.Strings(rejected)
	return accepted, rejected
}

// gmailHost reports whether an IMAP host, with or without a ":port", is Gmail.
func gmailHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(h, ":"); i >= 0 {
		h = h[:i]
	}
	return h == "imap.gmail.com" || h == "imap.googlemail.com"
}

// ApplyPresets adjusts already-sanitized options for servers that need it and
// returns a line for the job log per change, so a run never differs silently
// from what was asked for.
//
// Gmail shows every label as a folder, so one message sits in INBOX, All Mail,
// Important and Starred at once. Copied as-is to a server with real folders
// that is three or four copies of each message, which a threaded webmail then
// lists side by side. --gmail1 copies each message once (All Mail goes last and
// only receives what no other folder had) and drops the "[Gmail]/" prefix.
// Gmail to Gmail is left alone: labels mean the same thing on both sides.
func ApplyPresets(host1, host2 string, opts map[string]any) []string {
	var notes []string
	if gmailHost(host1) && !gmailHost(host2) && opts["gmail1"] != true && opts["gmail2"] != true {
		opts["gmail1"] = true
		notes = append(notes, "Note: source is Gmail, added --gmail1 so a message under several labels is copied once")
	}
	// imapsync exits with a usage error when given both, and --gmail1 turns
	// --skipcrossduplicates on.
	if (opts["gmail1"] == true || opts["skipcrossduplicates"] == true) && opts["usecache"] == true {
		delete(opts, "usecache")
		notes = append(notes, "Note: dropped --usecache, imapsync cannot combine it with --skipcrossduplicates")
	}
	return notes
}

func truthy(v any) (on bool, valid bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
		// An empty string is not "yes". It used to be, so an option arriving
		// with no value -- which is how ParseOptionString represents a bare
		// "--delete2" it could not pair -- silently enabled deletion on the
		// destination. A flag must be asked for explicitly.
	}
	return false, false
}

func scalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case float64: // JSON numbers decode as float64
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t)), true
		}
		return fmt.Sprintf("%v", t), true
	case int:
		return fmt.Sprintf("%d", t), true
	case int64:
		return fmt.Sprintf("%d", t), true
	}
	return "", false
}

// ValidateCron checks a five-field crontab expression, the dialect the Python
// scheduler accepted.
func ValidateCron(expr string) error {
	_, err := cron.ParseStandard(expr)
	return err
}

// floatOptions are the allowlisted numeric options imapsync declares as "=f".
// Everything else numeric is an integer count or a port.
var floatOptions = map[string]bool{
	"maxage": true, "minage": true,
	"maxmessagespersecond": true,
	"timeout":              true, "timeout1": true, "timeout2": true,
}
