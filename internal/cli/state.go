package cli

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// stateWrite is one POST /v1/state:batch item.
type stateWrite struct {
	OpID       string    `json:"op_id"`
	Principal  int64     `json:"principal"`
	EntryID    int64     `json:"entry_id"`
	Register   string    `json:"register"`
	Value      any       `json:"value"`
	Base       revision  `json:"base_rev"`
	OccurredAt time.Time `json:"occurred_at"`
	Kind       string    `json:"kind"`
}

// state applies one deliberate register change per entry id, each with its own
// op_id so a queued retry deduplicates without swallowing sibling writes.
func (a *app) state(ctx context.Context, action stateAction, args []string) error {
	fs := newFlagSet(string(action))
	if err := fs.Parse(args); err != nil {
		return usagef("usage: earnest %s <id>...", action)
	}
	ids, err := parseIDs(fs.Args())
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	generation, err := c.bootstrapCursor(ctx)
	if err != nil {
		return err
	}
	principal, err := c.principalID(ctx)
	if err != nil {
		return err
	}
	register, value := action.registerValue()
	if register == "" {
		return usagef("unknown state action %q", action)
	}
	writes := make([]stateWrite, 0, len(ids))
	for _, id := range ids {
		opID, err := newOpID()
		if err != nil {
			return err
		}
		writes = append(writes, stateWrite{
			OpID:       opID,
			Principal:  principal,
			EntryID:    id,
			Register:   register,
			Value:      value,
			Base:       revision{Generation: generation.Generation, Rev: 0},
			OccurredAt: nowFunc().UTC(),
			Kind:       "explicit",
		})
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/state:batch", nil, writes)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var results []stateResult
	if err := decode(raw, &results); err != nil {
		return err
	}
	rows := tableRows(len(results))
	for _, result := range results {
		rows = append(rows, []string{
			strconv.FormatInt(result.State.EntryID, 10),
			result.OpID,
			result.Status,
			result.State.Seen.Value.String(),
			result.State.Shelf.Value.String(),
			result.State.Kept.Value.String(),
		})
	}
	writeTable(a.out, []string{"ENTRY", "OP_ID", "STATUS", "SEEN", "SHELF", "KEPT"}, rows)
	return nil
}
