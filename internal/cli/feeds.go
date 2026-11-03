package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const opmlContentType = "application/xml"

func (a *app) feeds(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: earnest feeds list|add <url>|remove <id>|import <opml>|export")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return a.feedsList(ctx, c, args[1:])
	case "add":
		return a.feedsAdd(ctx, c, args[1:])
	case "remove":
		return a.feedsRemove(ctx, c, args[1:])
	case "import":
		return a.feedsImport(ctx, c, args[1:])
	case "export":
		return a.feedsExport(ctx, c, args[1:])
	default:
		return usagef("unknown feeds command %q", args[0])
	}
}

func (a *app) feedsList(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("feeds list")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest feeds list")
	}
	raw, err := c.get(ctx, "/v1/subscriptions", nil)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var subscriptions []subscription
	if err := decode(raw, &subscriptions); err != nil {
		return err
	}
	rows := tableRows(len(subscriptions))
	for _, sub := range subscriptions {
		rows = append(rows, []string{
			fmt.Sprint(sub.ID),
			cell(sub.Title),
			cell(sub.URL),
			sub.Cadence,
			healthCell(sub),
		})
	}
	writeTable(a.out, []string{"ID", "TITLE", "URL", "CADENCE", "HEALTH"}, rows)
	return nil
}

func healthCell(sub subscription) string {
	if sub.Health.ConsecutiveFailures > 0 {
		return fmt.Sprintf("failing(%d)", sub.Health.ConsecutiveFailures)
	}
	return "ok"
}

func (a *app) feedsAdd(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("feeds add")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest feeds add <url>")
	}
	feedURL := fs.Arg(0)
	if !strings.HasPrefix(feedURL, "http://") && !strings.HasPrefix(feedURL, "https://") {
		return usagef("feed url must start with http:// or https://")
	}
	opID, err := newOpID()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/subscriptions", nil, subscribeBody{URL: feedURL, OpID: opID})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var sub subscription
	if err := decode(raw, &sub); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "subscribed %s (id %d)\n", cell(sub.Title), sub.ID)
	return nil
}

type subscribeBody struct {
	URL  string `json:"url"`
	OpID string `json:"op_id"`
}

func (a *app) feedsRemove(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("feeds remove")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest feeds remove <id>")
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}
	opID, err := newOpID()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodDelete, fmt.Sprintf("/v1/subscriptions/%d", id), url.Values{"op_id": {opID}}, nil)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	fmt.Fprintf(a.out, "removed subscription %d\n", id)
	return nil
}

func (a *app) feedsImport(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("feeds import")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest feeds import <opml>")
	}
	body, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("read OPML: %w", err)
	}
	raw, err := c.raw(ctx, http.MethodPost, "/v1/opml:import", opmlContentType, body)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var results []opmlResult
	if err := decode(raw, &results); err != nil {
		return err
	}
	rows := tableRows(len(results))
	for _, result := range results {
		rows = append(rows, []string{outlinePath(result.Path), outcomeCell(result)})
	}
	writeTable(a.out, []string{"OUTLINE", "RESULT"}, rows)
	return nil
}

func outlinePath(path []int) string {
	if len(path) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(path))
	for _, index := range path {
		parts = append(parts, fmt.Sprint(index))
	}
	return strings.Join(parts, "/")
}

func outcomeCell(result opmlResult) string {
	switch {
	case result.Error != "":
		return "error: " + result.Error
	case result.Subscription != nil:
		title := result.Subscription.Title
		if title == "" {
			title = result.Subscription.URL
		}
		if result.Deferred {
			return "subscribed (fetching): " + title
		}
		return "subscribed: " + title
	case result.Folder != nil:
		return "folder: " + result.Folder.Name
	default:
		return "skipped"
	}
}

// feedsExport writes the OPML document itself; it is already machine-readable,
// so --json does not wrap it.
func (a *app) feedsExport(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("feeds export")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest feeds export")
	}
	raw, err := c.get(ctx, "/v1/opml", nil)
	if err != nil {
		return err
	}
	_, err = a.out.Write(raw)
	return err
}
