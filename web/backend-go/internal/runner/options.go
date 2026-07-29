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
	// performance knobs
	"parallel": kindInt, "batchsize": kindInt, "pipeline": kindInt,
	"condstore": kindFlag, "nocondstore": kindFlag,
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
		if kind == kindInt && !digits.MatchString(text) {
			rejected = append(rejected, key)
			continue
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

func truthy(v any) (on bool, valid bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(t) {
		case "1", "true", "yes", "on", "":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
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
