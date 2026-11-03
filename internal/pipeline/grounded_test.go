package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
)

func TestGroundedOutputClosedAndVerified(t *testing.T) {
	hash := strings.Repeat("a", 64)
	sources := []Source{{EntryID: 1, ContentHash: hash, Text: "Beacon launched.\n The trial lasts a week. Source: https://example.org/a"}}
	req, err := GroundedRequest(llm.TaskAsk, "What launched?", sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, text, quote, version string
		wantErr                    bool
		verified                   bool
	}{
		{name: "normalized", text: "Beacon launched.", quote: "Beacon launched. The trial lasts a week.", version: hash, verified: true},
		{name: "fabricated", text: "Claim remains visible", quote: "Not published", version: hash},
		{name: "old version", text: "Beacon launched", quote: "Beacon launched.", version: strings.Repeat("b", 64)},
		{name: "source link", text: "Read https://example.org/a", quote: "Beacon launched.", version: hash, verified: true},
		{name: "new link", text: "Read https://example.org/ab", quote: "Beacon launched.", version: hash, wantErr: true},
		{name: "image", text: "<img src=beacon>", quote: "Beacon launched.", version: hash, wantErr: true},
		{name: "markdown image", text: "![x](beacon)", quote: "Beacon launched.", version: hash, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"claims": []any{map[string]any{"text": test.text, "citations": []any{map[string]any{"entry_id": 1, "content_hash": test.version, "passages": []string{test.quote}}}}}})
			claims, err := DecodeGrounded(req, string(raw), sources)
			if test.wantErr {
				if !errors.Is(err, llm.ErrInvalidOutput) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || len(claims) != 1 || claims[0].Text != test.text || claims[0].Verified != test.verified {
				t.Fatalf("claims %+v %v", claims, err)
			}
		})
	}
	if _, err := DecodeGrounded(req, `{"claims":[],"tools":["fetch"]}`, sources); !errors.Is(err, llm.ErrInvalidOutput) {
		t.Fatal("extra property accepted", err)
	}
}

type goldenFake struct{ calls int }

func (f *goldenFake) Complete(_ context.Context, _ llm.Task, req llm.CompleteRequest) (*llm.Completion, error) {
	f.calls++
	text := req.Messages[0].Content
	claims := []map[string]any{}
	if req.Schema.Name == "summary" {
		for range 3 {
			claims = append(claims, map[string]any{"text": "Beacon trial.", "passages": []string{text}})
		}
		data, _ := json.Marshal(map[string]any{"claims": claims})
		return &llm.Completion{Text: string(data)}, nil
	}
	return nil, llm.ErrDisabled
}
func (*goldenFake) Classify(context.Context, Content) ([]string, error) {
	return []string{LabelNews}, nil
}
func TestGoldenCorpusAndRubric(t *testing.T) {
	file, err := os.Open("../../testdata/golden/items.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	items, err := LoadGolden(file)
	if err != nil || len(items) != 30 {
		t.Fatalf("corpus %d %v", len(items), err)
	}
	excerpt, injection, code, long := 0, 0, 0, 0
	for _, item := range items {
		if item.ExcerptOnly {
			excerpt++
		}
		if strings.Contains(strings.ToLower(item.Text), "ignore") {
			injection++
		}
		if strings.Contains(item.Text, "`") {
			code++
		}
		if len(item.Text) > 600 {
			long++
		}
	}
	if excerpt < 4 || injection < 3 || code < 2 || long < 2 {
		t.Fatalf("corpus diversity %d %d %d %d", excerpt, injection, code, long)
	}
	fake := &goldenFake{}
	item := GoldenItem{ID: "case", Text: "Beacon trial.", Rubric: Rubric{MustMention: []string{"Beacon"}, MustNotMention: []string{"buy"}}}
	report, err := Evaluate(context.Background(), fake, llm.TaskSummary, []GoldenItem{item})
	if err != nil || report.Passed != 1 || report.VerifiedClaims != 3 || report.CitationRate == nil || *report.CitationRate != 1 {
		t.Fatalf("report %+v %v", report, err)
	}
	item.Rubric.MustNotMention = []string{"trial"}
	report, err = Evaluate(context.Background(), fake, llm.TaskSummary, []GoldenItem{item})
	if err != nil || report.Passed != 0 {
		t.Fatal("forbidden rubric ignored", report, err)
	}
}
