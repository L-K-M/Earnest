package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestReciprocalRankFusion(t *testing.T) {
	lexical := []SearchResult{{EntryID: 1, Seq: 1, Rank: 100}, {EntryID: 2, Seq: 2, Rank: 1}, {EntryID: 1, AnnotationID: 9, AgentName: "Scout", Seq: 1}}
	semantic := []SearchResult{{EntryID: 2, Seq: 2, Rank: .99}, {EntryID: 3, Seq: 3, Rank: .98}}
	got := fuseResults(lexical, semantic)
	if len(got) != 4 || got[0].EntryID != 2 || got[0].AnnotationID != 0 {
		t.Fatalf("fusion %+v", got)
	}
	for _, hit := range got {
		if hit.AnnotationID == 9 && hit.Rank != 1.0/63 {
			t.Fatalf("note received vector credit: %+v", hit)
		}
	}
}

func TestHybridSimilarAndSemanticRules(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "beacon news")
	other := f.ingest(t, "same topic without lexical word")
	sports := f.ingest(t, "sports scores")
	var agent int64
	ctx := context.Background()
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Scout') RETURNING id`, f.a.UserID).Scan(&agent); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev) VALUES($1,$2,'note','beacon AGENT_NOTE_ONLY',ARRAY[$3::bigint],1)`, f.a.UserID, agent, sports); err != nil {
		t.Fatal(err)
	}
	p := &pipelineProvider{classifier: `{"labels":["news"]}`}
	ctx = pipelineTestContext(t, f.db, p, []llm.Task{llm.TaskEmbed}, 0)
	if _, err := f.s.RunPipeline(ctx, 10); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.Search(ctx, f.a, "beacon", SearchFilter{Mode: SearchHybrid}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].EntryID != id || got[0].AnnotationID != 0 {
		t.Fatalf("hybrid %+v", got)
	}
	var note bool
	for _, hit := range got {
		if hit.AnnotationID != 0 {
			note = hit.AgentName == "Scout" && strings.Contains(hit.Snippet, "AGENT_NOTE_ONLY")
		}
	}
	if !note {
		t.Fatalf("missing labelled lexical note %+v", got)
	}
	calls := len(p.requests())
	more, err := f.s.Similar(ctx, f.a, id, 10)
	if err != nil || len(more) != 2 || more[0].EntryID != other {
		t.Fatalf("similar %+v %v", more, err)
	}
	if len(p.requests()) != calls {
		t.Fatal("similar made a model call")
	}
	// Query descriptions are embedded once and kept separate from article bodies.
	predicate := RulePredicate{Type: RuleSemantic, Value: "beacon topic"}
	ids, _, err := f.s.DryRunRule(ctx, f.a, predicate, []RuleAction{{Type: RuleNotify}})
	if err != nil || len(ids) != 2 {
		t.Fatalf("semantic rule %v %v", ids, err)
	}
	calls = len(p.requests())
	if _, _, err := f.s.DryRunRule(ctx, f.a, predicate, []RuleAction{{Type: RuleNotify}}); err != nil {
		t.Fatal(err)
	}
	if len(p.requests()) != calls {
		t.Fatal("semantic rule description embedded twice")
	}
	first, err := f.s.Search(ctx, f.a, "beacon", SearchFilter{Mode: SearchHybrid, Limit: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor := &SearchCursor{Rank: first[0].Rank, Seq: first[0].Seq, AnnotationID: first[0].AnnotationID}
	next, err := f.s.Search(ctx, f.a, "beacon", SearchFilter{Mode: SearchHybrid, Limit: 1}, cursor)
	if err != nil || len(next) != 1 || (next[0].EntryID == first[0].EntryID && next[0].AnnotationID == first[0].AnnotationID) {
		t.Fatalf("hybrid pagination %+v %v", next, err)
	}
	for _, request := range p.requests() {
		if strings.Contains(request, "AGENT_NOTE_ONLY") {
			t.Fatal("annotation entered provider request")
		}
	}
	disabled, _, err := f.s.DryRunRule(context.Background(), f.a, RulePredicate{Type: RuleNot, Children: []RulePredicate{{Type: RuleSemantic, Value: "beacon"}}}, []RuleAction{{Type: RuleNotify}})
	if err != nil || len(disabled) != 0 {
		t.Fatalf("negated disabled model matched: %v %v", disabled, err)
	}
	if _, err := f.s.Similar(context.Background(), f.a, id, 1); !errors.Is(err, ErrPipelineDisabled) {
		t.Fatalf("disabled similar: %v", err)
	}
}
