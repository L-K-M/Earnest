package cli

import (
	"context"
	"fmt"
	"net/http"
)

type triageBody struct {
	OpID       string  `json:"op_id"`
	Generation string  `json:"generation"`
	EntryIDs   []int64 `json:"entry_ids"`
	Action     string  `json:"action"`
}

type revertBody struct {
	RevertHandle string `json:"revert_handle"`
	OpID         string `json:"op_id"`
}

// triage changes human state as an agent and returns the expiring revert
// handle, which is printed so the operator can undo the action.
func (a *app) triage(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: earnest triage done|seen <id>...")
	}
	action := triageAction(args[0])
	if !validTriageAction(action) {
		return usagef("invalid triage action %q", args[0])
	}
	fs := newFlagSet("triage " + string(action))
	if err := fs.Parse(args[1:]); err != nil {
		return usagef("usage: earnest triage done|seen <id>...")
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
	opID, err := newOpID()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/triage", nil, triageBody{
		OpID:       opID,
		Generation: generation.Generation,
		EntryIDs:   ids,
		Action:     string(action),
	})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var result triageResult
	if err := decode(raw, &result); err != nil {
		return err
	}
	for _, item := range result.Results {
		fmt.Fprintf(a.out, "entry %d: %s\n", item.State.EntryID, item.Status)
	}
	if result.RevertHandle != "" {
		fmt.Fprintf(a.out, "revert handle: %s\n", result.RevertHandle)
		fmt.Fprintf(a.out, "undo with: earnest revert %s\n", result.RevertHandle)
		fmt.Fprintf(a.out, "expires %s\n", formatTime(result.ExpiresAt))
	}
	return nil
}

func (a *app) revert(ctx context.Context, args []string) error {
	fs := newFlagSet("revert")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest revert <handle>")
	}
	handle := fs.Arg(0)
	c, err := a.client()
	if err != nil {
		return err
	}
	opID, err := newOpID()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/triage:revert", nil, revertBody{RevertHandle: handle, OpID: opID})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	fmt.Fprintf(a.out, "reverted %s\n", handle)
	return nil
}
