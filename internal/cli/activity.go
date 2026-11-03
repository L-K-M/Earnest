package cli

import (
	"context"
	"net/url"
	"strconv"
)

const defaultActivityLimit = 100

func (a *app) activity(ctx context.Context, args []string) error {
	limit := defaultActivityLimit
	fs := newFlagSet("activity")
	action := fs.String("action", "", "only this action")
	principal := fs.Int64("principal", 0, "only this principal")
	since := fs.String("since", "", "RFC 3339 lower bound")
	fs.IntVar(&limit, "limit", defaultActivityLimit, "maximum rows")
	if err := parseFlags(fs, args, map[string]bool{"action": true, "principal": true, "since": true, "limit": true}); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest activity [--limit n]")
	}
	if limit < 1 || limit > 1000 {
		return usagef("--limit must be between 1 and 1000")
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if *action != "" {
		query.Set("action", *action)
	}
	if *principal > 0 {
		query.Set("principal_id", strconv.FormatInt(*principal, 10))
	}
	if *since != "" {
		query.Set("since", *since)
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	raw, err := c.get(ctx, "/v1/activity", query)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var rowsData []activity
	if err := decode(raw, &rowsData); err != nil {
		return err
	}
	rows := tableRows(len(rowsData))
	for _, item := range rowsData {
		rows = append(rows, []string{
			strconv.FormatInt(item.ID, 10),
			formatTime(item.CreatedAt),
			cell(item.PrincipalName),
			item.Action,
			item.NextAction,
			compactJSON(item.Target),
		})
	}
	writeTable(a.out, []string{"ID", "TIME", "PRINCIPAL", "ACTION", "NEXT", "TARGET"}, rows)
	return nil
}
