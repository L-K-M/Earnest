package cli

import (
	"context"
	"fmt"
	"net/http"
)

type annotationBody struct {
	EntryIDs []int64 `json:"entry_ids"`
	Value    string  `json:"value"`
	OpID     string  `json:"op_id"`
}

// annotate writes one annotation over the given entries. The API keys an
// annotation by author, kind and canonical entry set, so one request covers
// all ids.
func (a *app) annotate(ctx context.Context, args []string) error {
	kind := kindNote
	fs := newFlagSet("annotate")
	text := fs.String("text", "", "annotation text")
	fs.StringVar((*string)(&kind), "kind", string(kindNote), "kind: note|label|score|brief")
	if err := parseFlags(fs, args, map[string]bool{"kind": true, "text": true}); err != nil {
		return usagef("usage: earnest annotate <id>... --kind note|label|score|brief --text <text>")
	}
	if !validAnnotationKind(kind) {
		return usagef("invalid --kind %q", string(kind))
	}
	if *text == "" {
		return usagef("--text is required")
	}
	ids, err := parseIDs(fs.Args())
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	return a.putAnnotation(ctx, c, kind, ids, *text)
}

// brief is a brief annotation: the newest one per principal appears at the top
// of Today.
func (a *app) brief(ctx context.Context, args []string) error {
	fs := newFlagSet("brief")
	text := fs.String("text", "", "brief text")
	if err := parseFlags(fs, args, map[string]bool{"text": true}); err != nil {
		return usagef("usage: earnest brief --text <text> <id>...")
	}
	if *text == "" {
		return usagef("--text is required")
	}
	ids, err := parseIDs(fs.Args())
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	return a.putAnnotation(ctx, c, kindBrief, ids, *text)
}

func (a *app) putAnnotation(ctx context.Context, c *client, kind annotationKind, ids []int64, text string) error {
	opID, err := newOpID()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodPut, "/v1/annotations/"+string(kind), nil, annotationBody{
		EntryIDs: ids,
		Value:    text,
		OpID:     opID,
	})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var result annotation
	if err := decode(raw, &result); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s recorded on %d entries (id %d)\n", kind, len(ids), result.ID)
	return nil
}
