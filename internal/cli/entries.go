package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const defaultEntryLimit = 50
const maxEntryLimit = 200

func (a *app) entries(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: earnest entries list|import")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return a.entriesList(ctx, c, args[1:])
	case "import":
		return a.entriesImport(ctx, c, args[1:])
	default:
		return usagef("unknown entries command %q", args[0])
	}
}

type entryFilter struct {
	view   viewFilter
	unread bool
	feed   int64
	limit  int
	cursor string
}

func (a *app) entriesList(ctx context.Context, c *client, args []string) error {
	filter := entryFilter{view: viewToday, limit: defaultEntryLimit}
	fs := newFlagSet("entries list")
	fs.StringVar((*string)(&filter.view), "view", string(viewToday), "view: today|later|kept|done|all")
	fs.BoolVar(&filter.unread, "unread", false, "only unread entries")
	fs.Int64Var(&filter.feed, "feed", 0, "subscription id")
	fs.IntVar(&filter.limit, "limit", defaultEntryLimit, "page size")
	fs.StringVar(&filter.cursor, "cursor", "", "exclusive seq cursor from the previous page")
	if err := parseFlags(fs, args, map[string]bool{"view": true, "feed": true, "limit": true, "cursor": true}); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest entries list [--view today|later|kept|done|all] [--unread] [--feed id] [--limit n]")
	}
	if !validView(filter.view) {
		return usagef("invalid --view %q", string(filter.view))
	}
	if filter.feed < 0 || filter.limit < 1 || filter.limit > maxEntryLimit {
		return usagef("--limit must be between 1 and %d", maxEntryLimit)
	}
	query := url.Values{}
	query.Set("view", filter.view.apiValue())
	if filter.unread {
		query.Set("unread_only", "true")
	}
	if filter.feed > 0 {
		query.Set("subscription_id", strconv.FormatInt(filter.feed, 10))
	}
	query.Set("limit", strconv.Itoa(filter.limit))
	if filter.cursor != "" {
		query.Set("cursor", filter.cursor)
	}
	raw, err := c.get(ctx, "/v1/entries", query)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var page entriesPage
	if err := decode(raw, &page); err != nil {
		return err
	}
	a.printEntries(page)
	return nil
}

// printEntries renders an entry page and its continuation hint.
func (a *app) printEntries(page entriesPage) {
	rows := tableRows(len(page.Entries))
	for _, item := range page.Entries {
		published := "-"
		if item.PublishedAt != nil {
			published = formatTime(*item.PublishedAt)
		}
		rows = append(rows, []string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.SubscriptionID, 10),
			cell(item.Title),
			published,
			item.State.label(),
		})
	}
	writeTable(a.out, []string{"ID", "SUB", "TITLE", "PUBLISHED", "STATE"}, rows)
	if page.NextCursor != "" {
		fmt.Fprintf(a.out, "more: --cursor %s\n", page.NextCursor)
	}
}

// keptImportSummary orders the summary counts; "not subscribed" is the
// human label for the not_subscribed status.
var keptImportSummary = []struct {
	status string
	label  string
}{
	{"created", "created"},
	{"matched", "matched"},
	{"unchanged", "unchanged"},
	{"unkept", "unkept"},
	{"not_subscribed", "not subscribed"},
	{"gone", "gone"},
	{"failed", "failed"},
}

func (a *app) entriesImport(ctx context.Context, c *client, args []string) error {
	subscribeMissing := false
	fs := newFlagSet("entries import")
	fs.BoolVar(&subscribeMissing, "subscribe-missing", false, "subscribe to feeds you don't follow, then import their articles")
	if err := parseFlags(fs, args, nil); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest entries import [--subscribe-missing] <export.json>")
	}
	body, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("read export: %w", err)
	}
	var query url.Values
	if subscribeMissing {
		query = url.Values{"missing_feeds": {"subscribe"}}
	}
	raw, err := c.do(ctx, http.MethodPost, "/v1/kept:import", query, "application/json", body)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var report keptImportReport
	if err := decode(raw, &report); err != nil {
		return err
	}
	a.printKeptImport(report, subscribeMissing)
	return nil
}

// printKeptImport renders the per-article table, the subscriptions the import
// created, the status summary and the --subscribe-missing hint.
func (a *app) printKeptImport(report keptImportReport, subscribeMissing bool) {
	rows := tableRows(len(report.Results))
	counts := map[string]int{}
	for _, result := range report.Results {
		counts[result.Status]++
		title := result.Title
		if title == "" {
			title = "-"
		}
		rows = append(rows, []string{strconv.Itoa(result.Index), keptResultCell(result), cell(title)})
	}
	writeTable(a.out, []string{"ARTICLE", "RESULT", "TITLE"}, rows)
	for _, sub := range report.Subscribed {
		title := sub.Title
		if title == "" {
			title = sub.URL
		}
		fmt.Fprintf(a.out, "subscribed: %s\n", title)
	}
	if len(report.Results) == 0 {
		fmt.Fprintln(a.out, "summary: nothing to import")
	} else {
		parts := make([]string, 0, len(keptImportSummary))
		for _, item := range keptImportSummary {
			if counts[item.status] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[item.status], item.label))
			}
		}
		fmt.Fprintf(a.out, "summary: %s\n", strings.Join(parts, ", "))
	}
	if !subscribeMissing && counts["not_subscribed"] > 0 {
		fmt.Fprintf(a.out, "%d skipped because you don't follow their feed; run again with --subscribe-missing to subscribe and import them\n", counts["not_subscribed"])
	}
}

// keptResultCell renders one result cell: the error for failures, the feed URL
// for skipped feeds, or the status with underscores turned into spaces.
func keptResultCell(result keptImportResult) string {
	switch result.Status {
	case "failed":
		if result.Error == "" {
			return "failed"
		}
		return "failed: " + result.Error
	case "not_subscribed":
		if result.FeedURL != "" {
			return "not subscribed: " + result.FeedURL
		}
		return "not subscribed"
	default:
		return strings.ReplaceAll(result.Status, "_", " ")
	}
}

func (a *app) read(ctx context.Context, args []string) error {
	fs := newFlagSet("read")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest read <id>")
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	raw, err := c.get(ctx, fmt.Sprintf("/v1/entries/%d/content", id), nil)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var body content
	if err := decode(raw, &body); err != nil {
		return err
	}
	// Terminate the output so a shell prompt never lands on the last line.
	if !strings.HasSuffix(body.Text, "\n") {
		_, err = fmt.Fprint(a.out, body.Text+"\n")
		return err
	}
	_, err = fmt.Fprint(a.out, body.Text)
	return err
}

func (a *app) search(ctx context.Context, args []string) error {
	fs := newFlagSet("search")
	limit := fs.Int("limit", defaultEntryLimit, "maximum hits")
	feed := fs.Int64("feed", 0, "subscription id")
	folder := fs.Int64("folder", 0, "folder id")
	cursor := fs.String("cursor", "", "exclusive cursor from the previous page")
	if err := parseFlags(fs, args, map[string]bool{"limit": true, "feed": true, "folder": true, "cursor": true}); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest search [--feed id] [--folder id] [--limit n] [--cursor c] <query>")
	}
	if *limit < 1 || *limit > maxEntryLimit {
		return usagef("--limit must be between 1 and %d", maxEntryLimit)
	}
	if *feed < 0 || *folder < 0 {
		return usagef("--feed and --folder must be positive")
	}
	query := strings.TrimSpace(fs.Arg(0))
	if query == "" {
		return usagef("search query must not be empty")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("limit", strconv.Itoa(*limit))
	if *feed > 0 {
		values.Set("subscription_id", strconv.FormatInt(*feed, 10))
	}
	if *folder > 0 {
		values.Set("folder_id", strconv.FormatInt(*folder, 10))
	}
	if *cursor != "" {
		values.Set("cursor", *cursor)
	}
	raw, err := c.get(ctx, "/v1/search", values)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var page searchPage
	if err := decode(raw, &page); err != nil {
		return err
	}
	a.printSearchPage(page)
	return nil
}

// printSearchPage renders a search page and its continuation hint.
func (a *app) printSearchPage(page searchPage) {
	rows := tableRows(len(page.Matches))
	for _, hit := range page.Matches {
		title := hit.Title
		if title == "" && hit.Kind == "agent_note" {
			title = "agent note"
		}
		rows = append(rows, []string{
			strconv.FormatInt(hit.EntryID, 10),
			cell(title),
			cell(hit.AgentName),
			cell(hit.Snippet),
		})
	}
	writeTable(a.out, []string{"ID", "TITLE", "AGENT", "SNIPPET"}, rows)
	if page.NextCursor != "" {
		fmt.Fprintf(a.out, "more: --cursor %s\n", page.NextCursor)
	}
}

// briefing lists what is new for this principal since its position in the
// chosen view. It composes the position and entry endpoints so nothing is
// marked seen by reading.
func (a *app) briefing(ctx context.Context, args []string) error {
	view := viewToday
	limit := defaultEntryLimit
	cursor := ""
	fs := newFlagSet("briefing")
	fs.StringVar((*string)(&view), "view", string(viewToday), "position view: today|later|kept|done|all")
	fs.IntVar(&limit, "limit", defaultEntryLimit, "maximum entries")
	fs.StringVar(&cursor, "cursor", "", "exclusive seq cursor from the previous page")
	if err := parseFlags(fs, args, map[string]bool{"view": true, "limit": true, "cursor": true}); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest briefing [--view today|later|kept|done|all] [--limit n] [--cursor c]")
	}
	if !validView(view) {
		return usagef("invalid --view %q", string(view))
	}
	if limit < 1 || limit > maxEntryLimit {
		return usagef("--limit must be between 1 and %d", maxEntryLimit)
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	positionView := view.apiValue()
	raw, err := c.get(ctx, "/v1/positions/"+url.PathEscape(positionView), nil)
	if err != nil {
		return err
	}
	var pos position
	if err := decode(raw, &pos); err != nil {
		return err
	}
	query := url.Values{}
	query.Set("view", positionView)
	if cursor != "" {
		query.Set("cursor", cursor)
	} else if pos.Seq > 0 {
		query.Set("after_seq", strconv.FormatUint(pos.Seq, 10))
	}
	query.Set("limit", strconv.Itoa(limit))
	raw, err = c.get(ctx, "/v1/entries", query)
	if err != nil {
		return err
	}
	var page entriesPage
	if err := decode(raw, &page); err != nil {
		return err
	}
	if a.json {
		return a.encodeJSON(struct {
			Position   position `json:"position"`
			Entries    []entry  `json:"entries"`
			NextCursor string   `json:"next_cursor,omitempty"`
		}{Position: pos, Entries: page.Entries, NextCursor: page.NextCursor})
	}
	fmt.Fprintf(a.out, "%d new since seq %d in %s\n", len(page.Entries), pos.Seq, positionView)
	a.printEntries(page)
	return nil
}
