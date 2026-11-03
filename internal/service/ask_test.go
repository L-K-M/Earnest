package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestAskPublisherOnlyRetrievalAndFailedCitations(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "Beacon launched. Testing continues."
	id := f.ingest(t, body)
	other := f.ingest(t, "Unrelated publisher article.")
	var agent int64
	if err := f.db.Pool().QueryRow(context.Background(), `INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Agent') RETURNING id`, f.a.UserID).Scan(&agent); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(context.Background(), `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev) VALUES($1,$2,'note','Beacon AGENT_NOTE_SENTINEL',ARRAY[$3::bigint],1)`, f.a.UserID, agent, other); err != nil {
		t.Fatal(err)
	}
	private := entriesTestFeed(t, f.db, "https://private.test/rss", f.a.UserID)
	entriesTestSub(t, f.db, f.a.UserID, private, "week")
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: private, Status: FetchOK, Items: []IngestItem{coreItem("Beacon PRIVATE_SENTINEL")}}); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{"ask": groundedOutput(id, coreHash("body"+body), "Beacon launched.", "Beacon launched.")}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskAsk}, 0)
	answer, err := f.s.Ask(ctx, f.a, "Beacon")
	if err == nil && (answer.DailyLimit != AskDailyBudget || answer.RemainingToday != AskDailyBudget-1) {
		t.Fatalf("ask budget %+v", answer)
	}
	if err != nil || len(answer.Sources) != 1 || answer.Sources[0].EntryID != id || !answer.Claims[0].Verified || answer.Retrieval != "fulltext" || !answer.Untrusted {
		t.Fatalf("answer %+v %v", answer, err)
	}
	for _, payload := range p.payloads() {
		for _, forbidden := range []string{"AGENT_NOTE_SENTINEL", "PRIVATE_SENTINEL", "Unrelated publisher", "\"tools\""} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("leaked %q", forbidden)
			}
		}
	}
	p.set("ask", groundedOutput(id, answer.Sources[0].ContentHash, "Fabricated claim remains.", "not in source"))
	answer, err = f.s.Ask(ctx, f.a, "Beacon")
	if err != nil || answer.RemainingToday != AskDailyBudget-2 || answer.Claims[0].Verified || answer.Claims[0].Text != "Fabricated claim remains." {
		t.Fatalf("fabricated %+v %v", answer, err)
	}
	p.set("ask", groundedOutput(id, answer.Sources[0].ContentHash, "Visit https://attacker.invalid/secret", "Beacon launched."))
	if _, err := f.s.Ask(ctx, f.a, "Beacon"); !errors.Is(err, llm.ErrInvalidOutput) {
		t.Fatal("injected link accepted", err)
	}
	if _, err := f.s.Ask(context.Background(), f.a, "Beacon"); !errors.Is(err, ErrPipelineDisabled) {
		t.Fatal("disabled ask", err)
	}
}

func TestAskDailyBudgetAtomic(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	p := &generatedProvider{outputs: map[string]string{}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskAsk}, 0)
	var wg sync.WaitGroup
	var mu sync.Mutex
	passed, paused := 0, 0
	for range AskDailyBudget + 5 {
		wg.Go(func() {
			_, err := f.s.Ask(ctx, f.a, "nonexistent")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				passed++
			} else if errors.Is(err, ErrPipelinePaused) {
				paused++
			} else {
				t.Errorf("ask %v", err)
			}
		})
	}
	wg.Wait()
	if passed != AskDailyBudget || paused != 5 || len(p.payloads()) != 0 {
		t.Fatalf("daily budget passed=%d paused=%d calls=%d", passed, paused, len(p.payloads()))
	}
	f.clock.Add(int64(24 * 60 * 60 * 1e9))
	if _, err := f.s.Ask(ctx, f.a, "nonexistent"); err != nil {
		t.Fatal("next UTC day", err)
	}
}

func TestAskPassageBound(t *testing.T) {
	text := strings.Repeat("芽", 2000) + " needle " + strings.Repeat("葉", 2000)
	passage := askPassage(text, "needle")
	if len(passage) > askPassageBytes || !strings.Contains(passage, "needle") || !pipeline.VerifyPassages(text, []string{passage}) {
		t.Fatal("bad retrieval passage")
	}
}

func TestAskUsesHybridRetrievalWhenEmbeddingsEnabled(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "An astronomical lamp is active."
	id := f.ingest(t, body)
	p := &generatedProvider{outputs: map[string]string{"ask": groundedOutput(id, coreHash("body"+body), "The lamp is active.", body)}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEmbed, llm.TaskAsk}, 0)
	if _, err := f.s.RunPipeline(ctx, 10); err != nil {
		t.Fatal(err)
	}
	out, err := f.s.Ask(ctx, f.a, "beacon")
	if err != nil || out.Retrieval != "hybrid" || len(out.Sources) != 1 || out.Sources[0].EntryID != id || !out.Claims[0].Verified {
		t.Fatalf("hybrid %+v %v", out, err)
	}
}
