package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

const defaultChangeLimit = 200

func (a *app) changes(ctx context.Context, args []string) error {
	cursor := ""
	limit := defaultChangeLimit
	fs := newFlagSet("changes")
	fs.StringVar(&cursor, "cursor", "", "generation:rev cursor from a previous page")
	fs.IntVar(&limit, "limit", defaultChangeLimit, "maximum changes")
	if err := parseFlags(fs, args, map[string]bool{"cursor": true, "limit": true}); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest changes [--cursor <generation:rev>] [--limit n]")
	}
	if limit < 1 || limit > 1000 {
		return usagef("--limit must be between 1 and 1000")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	fromHead := false
	if cursor == "" {
		// Without a cursor the current head is the only honest start; surface it
		// so the next run can resume from a saved older cursor.
		head, err := c.bootstrapCursor(ctx)
		if err != nil {
			return err
		}
		cursor = formatCursor(head)
		fromHead = true
	}
	query.Set("cursor", cursor)
	raw, err := c.get(ctx, "/v1/changes", query)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	if fromHead {
		fmt.Fprintf(a.out, "current cursor: %s\n", cursor)
	}
	var page changesPage
	if err := decode(raw, &page); err != nil {
		return err
	}
	if len(page.Changes) == 0 {
		fmt.Fprintln(a.out, "no changes")
	} else {
		rows := tableRows(len(page.Changes))
		for _, change := range page.Changes {
			rows = append(rows, []string{
				strconv.FormatUint(change.Rev, 10),
				change.Type,
				compactJSON(change.Payload),
			})
		}
		writeTable(a.out, []string{"REV", "TYPE", "PAYLOAD"}, rows)
	}
	if page.NextCursor != "" && page.NextCursor != cursor {
		fmt.Fprintf(a.out, "next cursor: %s\n", page.NextCursor)
	}
	return nil
}

func formatCursor(rev revision) string {
	return fmt.Sprintf("%s:%d", rev.Generation, rev.Rev)
}
