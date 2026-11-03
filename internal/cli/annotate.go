package cli

import (
	"context"
	"fmt"
	"net/http"
)

type annotationBody struct {
	Value string `json:"value"`
	OpID  string `json:"op_id"`
}

type briefBody struct {
	EntryIDs []int64 `json:"entry_ids"`
	Value    string  `json:"value"`
	OpID     string  `json:"op_id"`
}

// annotate writes an annotation over the given entries. Note, label and
// score are per-entry endpoints, so each id is one request; a brief covers
// the whole set in one request.
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
	if kind == kindBrief {
		return a.postBrief(ctx, c, ids, text)
	}
	results := make([]annotation, 0, len(ids))
	for _, id := range ids {
		opID, err := newOpID()
		if err != nil {
			return err
		}
		raw, err := c.send(ctx, http.MethodPut, fmt.Sprintf("/v1/entries/%d/annotations/%s", id, kind), nil, annotationBody{Value: text, OpID: opID})
		if err != nil {
			return err
		}
		var result annotation
		if err := decode(raw, &result); err != nil {
			return err
		}
		results = append(results, result)
		if !a.json {
			fmt.Fprintf(a.out, "%s recorded on entry %d (id %d)\n", kind, id, result.ID)
		}
	}
	if a.json {
		return a.encodeJSON(results)
	}
	return nil
}

// postBrief writes one brief over the whole entry set.
func (a *app) postBrief(ctx context.Context, c *client, ids []int64, text string) error {
	opID, err := newOpID()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/briefs", nil, briefBody{EntryIDs: ids, Value: text, OpID: opID})
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
	fmt.Fprintf(a.out, "brief recorded on %d entries (id %d)\n", len(ids), result.ID)
	return nil
}
