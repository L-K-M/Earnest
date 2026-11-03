package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

type notificationBody struct {
	OpID      string  `json:"op_id"`
	Note      string  `json:"note"`
	EntryIDs  []int64 `json:"entry_ids,omitempty"`
	Category  string  `json:"category"`
	DedupeKey string  `json:"dedupe_key,omitempty"`
	// Generation guards against a restore between queueing and sending; the
	// server rejects a stale one.
	Generation string `json:"generation,omitempty"`
}

func (a *app) notify(ctx context.Context, args []string) error {
	category := categoryAlert
	fs := newFlagSet("notify")
	note := fs.String("note", "", "notification text")
	entry := fs.Int64("entry", 0, "entry id the note is about")
	dedupe := fs.String("dedupe-key", "", "collapse repeats within the dedupe window")
	fs.StringVar((*string)(&category), "category", string(categoryAlert), "category: alert|digest|brief")
	if err := parseFlags(fs, args, map[string]bool{"note": true, "entry": true, "category": true, "dedupe-key": true}); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest notify --note <text> --entry <id> [--category alert]")
	}
	if *note == "" {
		return usagef("--note is required")
	}
	if !validNotificationCategory(category) {
		return usagef("invalid --category %q", string(category))
	}
	if *entry <= 0 {
		return usagef("--entry is required")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	opID, err := newOpID()
	if err != nil {
		return err
	}
	generation, err := c.bootstrapCursor(ctx)
	if err != nil {
		return err
	}
	body := notificationBody{
		OpID:       opID,
		Note:       *note,
		EntryIDs:   []int64{*entry},
		Category:   string(category),
		DedupeKey:  *dedupe,
		Generation: generation.Generation,
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/notifications", nil, body)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var result notification
	if err := decode(raw, &result); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "notification %d: %s\n", result.ID, result.Status)
	fmt.Fprintf(a.out, "budget: used %d/%d\n", result.Budget.UsedToday, result.Budget.LimitToday)
	if result.HeldUntil != nil {
		fmt.Fprintf(a.out, "held until %s\n", formatTime(*result.HeldUntil))
	}
	if len(result.EntryIDs) > 0 {
		fmt.Fprintf(a.out, "entries: %s\n", formatIDs(result.EntryIDs))
	}
	return nil
}

func formatIDs(ids []int64) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " "
		}
		out += strconv.FormatInt(id, 10)
	}
	return out
}
