// Package cli implements the earnest client commands over the REST API. It is
// the human-facing surface: device-flow login, feeds, entries, state, agent
// output and the change feed. It talks HTTP only, never the database.
package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Version identifies the client on the wire. The main package stamps it.
var Version = "dev"

const (
	exitOK = iota
	exitFailure
	exitUsage
)

// viewFilter selects an entry view. "today" is the undecided river with today
// decay, matching the API's "river".
type viewFilter string

const (
	viewToday viewFilter = "today"
	viewLater viewFilter = "later"
	viewKept  viewFilter = "kept"
	viewDone  viewFilter = "done"
	viewAll   viewFilter = "all"
)

func (v viewFilter) apiValue() string {
	if v == viewToday {
		return "river"
	}
	return string(v)
}

func validView(v viewFilter) bool {
	switch v {
	case viewToday, viewLater, viewKept, viewDone, viewAll:
		return true
	}
	return false
}

// annotationKind is the caller-writable subset of the annotation kinds.
type annotationKind string

const (
	kindNote  annotationKind = "note"
	kindLabel annotationKind = "label"
	kindScore annotationKind = "score"
	kindBrief annotationKind = "brief"
)

func validAnnotationKind(k annotationKind) bool {
	switch k {
	case kindNote, kindLabel, kindScore, kindBrief:
		return true
	}
	return false
}

// notificationCategory is the closed category set the server accepts.
type notificationCategory string

const (
	categoryAlert  notificationCategory = "alert"
	categoryDigest notificationCategory = "digest"
	categoryBrief  notificationCategory = "brief"
)

func validNotificationCategory(c notificationCategory) bool {
	switch c {
	case categoryAlert, categoryDigest, categoryBrief:
		return true
	}
	return false
}

// stateAction is one deliberate state change.
type stateAction string

const (
	actionDone   stateAction = "done"
	actionLater  stateAction = "later"
	actionKeep   stateAction = "keep"
	actionUnkeep stateAction = "unkeep"
	actionUnread stateAction = "unread"
)

// register and value are the write pair each action sends.
func (a stateAction) registerValue() (string, any) {
	switch a {
	case actionDone:
		return "shelf", "done"
	case actionLater:
		return "shelf", "later"
	case actionKeep:
		return "kept", true
	case actionUnkeep:
		return "kept", false
	case actionUnread:
		return "seen", "unseen"
	}
	return "", nil
}

// triageAction is the agent's human-state triage operation.
type triageAction string

const (
	triageSeen triageAction = "seen"
	triageDone triageAction = "done"
)

func validTriageAction(a triageAction) bool {
	return a == triageSeen || a == triageDone
}

// usageError marks a command line problem, which exits 2 rather than 1.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

type app struct {
	out    io.Writer
	errOut io.Writer
	json   bool
}

// Run dispatches one client command and returns the process exit code. It
// prints failures itself so callers only decide the code.
func Run(args []string, stdout, stderr io.Writer) int {
	args, jsonOut := extractJSONFlag(args)
	rawOPML := len(args) >= 2 && args[0] == "feeds" && args[1] == "export"
	if !jsonOut && !rawOPML {
		stdout = humanTextWriter{out: stdout}
	}
	stderr = humanTextWriter{out: stderr}
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}
	a := &app{out: stdout, errOut: stderr, json: jsonOut}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "login":
		err = a.login(ctx, args[1:])
	case "logout":
		err = a.logout(ctx, args[1:])
	case "whoami":
		err = a.whoami(ctx, args[1:])
	case "feeds":
		err = a.feeds(ctx, args[1:])
	case "entries":
		err = a.entries(ctx, args[1:])
	case "read":
		err = a.read(ctx, args[1:])
	case "search":
		err = a.search(ctx, args[1:])
	case "done", "later", "keep", "unkeep", "unread":
		err = a.state(ctx, stateAction(args[0]), args[1:])
	case "briefing":
		err = a.briefing(ctx, args[1:])
	case "annotate":
		err = a.annotate(ctx, args[1:])
	case "brief":
		err = a.brief(ctx, args[1:])
	case "notify":
		err = a.notify(ctx, args[1:])
	case "triage":
		err = a.triage(ctx, args[1:])
	case "revert":
		err = a.revert(ctx, args[1:])
	case "activity":
		err = a.activity(ctx, args[1:])
	case "saved-searches":
		err = a.savedSearches(ctx, args[1:])
	case "changes":
		err = a.changes(ctx, args[1:])
	case "help", "--help", "-h":
		printUsage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "earnest: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
	if err == nil {
		return exitOK
	}
	var u *usageError
	if errors.As(err, &u) {
		fmt.Fprintf(stderr, "earnest: %s\n", u.msg)
		return exitUsage
	}
	fmt.Fprintf(stderr, "earnest: %s\n", err)
	return exitFailure
}

// extractJSONFlag pulls the machine-output flag out of any position so both
// "earnest --json feeds list" and "earnest feeds list --json" work.
// Values and arguments after -- remain literal, even when they spell --json.
func extractJSONFlag(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if arg == "--json" || arg == "-json" {
			found = true
			continue
		}
		out = append(out, arg)
		name, hasValue := flagName(arg)
		if hasValue || i+1 == len(args) {
			continue
		}
		// These are the value-taking flags registered by the client commands.
		switch name {
		case "text", "kind", "note", "entry", "category", "dedupe-key", "view", "feed", "folder", "limit", "cursor", "action", "principal", "since":
			i++
			out = append(out, args[i])
		}
	}
	return out, found
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: earnest <command> [flags] [args]

Server: EARNEST_URL (default http://127.0.0.1:8080)
Token:  EARNEST_TOKEN or the config written by earnest login

  login                     device-flow login, stores the app token
  logout                    remove the stored token
  whoami                    show the authenticated identity (token masked)

  feeds list                list subscriptions
  feeds add <url>           subscribe to a feed
  feeds remove <id>         unsubscribe
  feeds import <opml>       import an OPML file
  feeds export              write the OPML subscription list

  entries list [flags]      list entries (--view, --unread, --feed, --limit, --cursor)
  entries import <file>     keep starred articles from a Google Reader JSON export (--subscribe-missing)
  read <id>                 print the stored plain text
  search [flags] <query>    search entries and notes (--feed, --folder, --limit, --cursor)
  briefing                  entries new since this principal's position

  done|later|keep|unkeep|unread <id>...
                            change state, one op_id per entry
  annotate <id>... --kind note|label|score|brief --text <text>
                            write an annotation over entries
  brief --text <text> <id>...
                            write a brief over entries

  saved-searches list       list named search views
  saved-searches add <name> <query> [--feed id] [--folder id] [--limit n]
                            save a named search view
  saved-searches show <id>  open a saved search (--cursor)
  saved-searches rename <id> <name>
                            rename a saved search
  saved-searches remove <id>
                            delete a saved search

  notify --note <text> --entry <id> [--category alert]
                            send a notification
  triage done|seen <id>...  change human state as an agent, returns a revert handle
  revert <handle>           undo a triage
  activity                  list agent and pipeline activity (--grouped)

  changes [--cursor <c>]    read the change feed

Every command accepts --json for machine output.
`)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses a command line that may intermix flags and positionals,
// which the documented forms "annotate <id>... --text ..." need. valueFlags
// names the flags that consume the following argument.
func parseFlags(fs *flag.FlagSet, args []string, valueFlags map[string]bool) error {
	ordered := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			ordered = append(ordered, "--")
			positional = append(positional, args[i+1:]...)
			break
		}
		name, hasValue := flagName(arg)
		if name == "" {
			positional = append(positional, arg)
			continue
		}
		ordered = append(ordered, arg)
		if !hasValue && valueFlags[name] && i+1 < len(args) {
			i++
			ordered = append(ordered, args[i])
		}
	}
	return fs.Parse(append(ordered, positional...))
}

// flagName splits a leading flag token into its name and whether it carries an
// inline value.
func flagName(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
		return "", false
	}
	name := strings.TrimLeft(arg, "-")
	if index := strings.IndexByte(name, '='); index >= 0 {
		return name[:index], true
	}
	return name, false
}

// parseIDs converts positional arguments to positive entry IDs.
func parseIDs(args []string) ([]int64, error) {
	if len(args) == 0 {
		return nil, usagef("at least one id is required")
	}
	ids := make([]int64, 0, len(args))
	for _, arg := range args {
		id, err := parseID(arg)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func parseID(arg string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || id <= 0 {
		return 0, usagef("invalid id %q", arg)
	}
	return id, nil
}

// newOpID returns a UUIDv7 from crypto/rand, so queued retries and concurrent
// commands never collide.
func newOpID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate op_id: %w", err)
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// writeJSON echoes a server response as indented JSON, falling back to raw
// bytes for non-JSON bodies such as OPML.
func (a *app) writeJSON(raw []byte) error {
	var out bytes.Buffer
	if err := json.Indent(&out, bytes.TrimSpace(raw), "", "  "); err != nil {
		// Not JSON (OPML, plain text): pass it through untouched.
		_, err := a.out.Write(raw)
		return err
	}
	out.WriteByte('\n')
	_, err := a.out.Write(out.Bytes())
	return err
}

func (a *app) encodeJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	data = append(data, '\n')
	_, err = a.out.Write(data)
	return err
}

// client loads configuration and refuses to run without a token.
func (a *app) client() (*client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	token := resolveToken(cfg)
	if token == "" {
		return nil, errors.New("not logged in: run \"earnest login\" or set EARNEST_TOKEN")
	}
	return newClient(resolveServer(cfg), token), nil
}

// sleepContext waits for d or until the context ends. Tests replace it.
var sleepContext = func(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

var nowFunc = time.Now
