package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

type testEditionBatch struct {
	submit, poll, cancel int
	ready                bool
	output               string
	requests             []llm.CompleteRequest
}

func (b *testEditionBatch) Submit(_ context.Context, req []llm.CompleteRequest) (string, error) {
	b.submit++
	b.requests = req
	return "batch-one", nil
}
func (b *testEditionBatch) Poll(_ context.Context, _ string, _ []llm.CompleteRequest) ([]string, bool, error) {
	b.poll++
	if !b.ready {
		return nil, false, nil
	}
	return []string{b.output}, true, nil
}
func (b *testEditionBatch) Cancel(context.Context, string) error { b.cancel++; return nil }

func TestEditionDeterministicDisabledAndWebhook(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "Weekly Beacon report.")
	if _, err := f.db.Pool().Exec(context.Background(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	hook, err := f.s.CreateWebhook(context.Background(), f.a, WebhookInput{URL: "https://hook.example/edition", Events: []string{"edition.ready"}}, "edition-hook")
	if err != nil {
		t.Fatal(err)
	}
	edition, err := f.s.TriggerEdition(context.Background(), f.a, EditionWeekly, time.Time{}, "edition-disabled")
	if err != nil || edition.Status != "list" || len(edition.Sections) != 1 || edition.Sections[0].Items[0].EntryID != id {
		t.Fatalf("list %+v %v", edition, err)
	}
	replay, err := f.s.TriggerEdition(context.Background(), f.a, EditionWeekly, time.Time{}, "edition-disabled")
	if err != nil || replay.ID != edition.ID {
		t.Fatalf("replay %+v %v", replay, err)
	}
	var count int
	if err := f.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE kind='webhook' AND (payload->>'webhook_id')::bigint=$1 AND payload->>'event'='edition.ready'`, hook.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("webhook %d %v", count, err)
	}
	if n, err := f.s.RunEditionJobs(context.Background(), 10); err != nil || n != 0 {
		t.Fatalf("disabled worker %d %v", n, err)
	}
	latest, err := f.s.LatestEdition(context.Background(), f.a)
	if err != nil || latest.ID != edition.ID {
		t.Fatal("latest", latest, err)
	}
}

func TestEditionBatchAndDeadlineFallback(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, ready := range []bool{true, false} {
		name := "batch"
		if !ready {
			name = "late fallback"
		}
		t.Run(name, func(t *testing.T) {
			f := newCoreFixture(t, template)
			body := "Weekly Beacon report."
			id := f.ingest(t, body)
			if _, err := f.db.Pool().Exec(context.Background(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
				t.Fatal(err)
			}
			output := groundedOutput(id, coreHash("body"+body), "Beacon report.", "Weekly Beacon report.")
			p := &generatedProvider{outputs: map[string]string{"edition": output, "summary": summaryOutput("Beacon report.", body)}}
			ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition, llm.TaskSummary}, 0)
			batch := &testEditionBatch{ready: ready, output: output}
			ctx = pipeline.WithEditionBatch(ctx, batch)
			original, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, f.s.now().Add(time.Hour), "edition-batch")
			if err != nil {
				t.Fatal(err)
			}
			if n, err := f.s.RunEditionJobs(ctx, 10); err != nil || n != 1 {
				t.Fatalf("submission %d %v", n, err)
			}
			if batch.submit != 1 || len(batch.requests) != 1 {
				t.Fatalf("batch %+v", batch)
			}
			// The edition sees raw publisher content, never the summary annotation.
			if strings.Contains(batch.requests[0].Messages[0].Content, `"summary"`) {
				t.Fatal("summary entered edition prompt")
			}
			f.clock.Add(int64(time.Hour))
			if n, err := f.s.RunEditionJobs(ctx, 10); err != nil || n != 1 {
				t.Fatalf("collection %d %v", n, err)
			}
			latest, err := f.s.LatestEdition(ctx, f.a)
			if err != nil || latest.ID != original.ID || latest.Status != "ready" || !latest.Sections[0].Claims[0].Verified || latest.Sections[0].Items[0].Summary == nil {
				t.Fatalf("ready %+v %v", latest, err)
			}
			if ready && batch.cancel != 0 {
				t.Fatal("completed batch cancelled")
			}
			if !ready && batch.cancel != 1 {
				t.Fatal("late batch not cancelled before fallback")
			}
			calls := len(p.payloads())
			if ready && calls != 1 || !ready && calls != 2 {
				t.Fatalf("calls=%d ready=%v", calls, ready)
			}
			// Turning the task off returns only the original deterministic paper.
			disabled, err := f.s.LatestEdition(context.Background(), f.a)
			if err != nil || disabled.Status != "list" || len(disabled.Sections[0].Claims) != 0 {
				t.Fatalf("disabled latest %+v %v", disabled, err)
			}
		})
	}
}

func TestEditionPrivateContentAndAgentNotesStayOut(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "Beacon news.")
	if _, err := f.db.Pool().Exec(context.Background(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	private := entriesTestFeed(t, f.db, "https://private.test/edition", f.a.UserID)
	entriesTestSub(t, f.db, f.a.UserID, private, "week")
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: private, Status: FetchOK, Items: []IngestItem{coreItem("EDITION_PRIVATE_SENTINEL")}}); err != nil {
		t.Fatal(err)
	}
	var agent int64
	if err := f.db.Pool().QueryRow(context.Background(), `INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Scout') RETURNING id`, f.a.UserID).Scan(&agent); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(context.Background(), `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev) VALUES($1,$2,'note','EDITION_NOTE_SENTINEL',ARRAY[$3::bigint],1)`, f.a.UserID, agent, id); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("bodyBeacon news."), "Beacon claim.", "fabrication")}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionDaily, time.Time{}, "edition-privacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	latest, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || latest.Status != "ready" || latest.Sections[0].Claims[0].Verified || latest.Sections[0].Claims[0].Text != "Beacon claim." {
		t.Fatalf("flagged %+v %v", latest, err)
	}
	for _, payload := range p.payloads() {
		if strings.Contains(payload, "EDITION_PRIVATE_SENTINEL") || strings.Contains(payload, "EDITION_NOTE_SENTINEL") || strings.Contains(payload, `"tools"`) {
			t.Fatal("edition leak", payload)
		}
	}
}

func TestEditionBudgetPauseKeepsJob(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "Beacon one.")
	if _, err := f.db.Pool().Exec(context.Background(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("bodyBeacon one."), "Beacon.", "Beacon one.")}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 1)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionDaily, time.Time{}, "spend"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	out, err := f.s.TriggerEdition(ctx, f.a, EditionDaily, time.Time{}, "paused")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	latest, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || latest.ID != out.ID || latest.Status != "paused" {
		t.Fatalf("pause %+v %v", latest, err)
	}
	var attempts int
	if err := f.db.Pool().QueryRow(ctx, `SELECT attempts FROM jobs WHERE kind='edition'`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("paused job attempts=%d %v", attempts, err)
	}
}

func TestCompiledAndAskDisabledMakeNoCalls(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	p := &generatedProvider{outputs: map[string]string{}}
	ctx := generatedContext(t, f, p, nil, 0)
	if _, err := f.s.CompileRule(ctx, f.a, "Notify news"); !errors.Is(err, ErrPipelineDisabled) {
		t.Fatal(err)
	}
	if _, err := f.s.Ask(ctx, f.a, "news"); !errors.Is(err, ErrPipelineDisabled) {
		t.Fatal(err)
	}
	if len(p.payloads()) != 0 {
		data, _ := json.Marshal(p.payloads())
		t.Fatal("disabled provider called", string(data))
	}
}

type cancellingBatch struct{ testEditionBatch }

func (b *cancellingBatch) Cancel(context.Context, string) error {
	b.cancel++
	return errors.New("cancellation unavailable")
}

func TestEditionLateCancellationFailureStillFallsBack(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "Beacon.")
	if _, err := f.db.Pool().Exec(context.Background(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("bodyBeacon."), "Beacon.", "Beacon.")}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	batch := &cancellingBatch{}
	ctx = pipeline.WithEditionBatch(ctx, batch)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, f.s.now().Add(time.Hour), "cancel-failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(time.Hour))
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	latest, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || latest.Status != "ready" || !strings.Contains(latest.Error, "cancellation") || len(latest.Sections[0].Claims) == 0 {
		t.Fatalf("fallback %+v %v", latest, err)
	}
}

func TestEditionTriggerReplayAfterDeadline(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	readyAt := f.s.now().Add(time.Hour)
	first, err := f.s.TriggerEdition(context.Background(), f.a, EditionDaily, readyAt, "retry-edition")
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(2 * time.Hour))
	replay, err := f.s.TriggerEdition(context.Background(), f.a, EditionDaily, readyAt, "retry-edition")
	if err != nil || replay.ID != first.ID {
		t.Fatalf("late retry %+v %v", replay, err)
	}
}

type blockingEditionBatch struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (b *blockingEditionBatch) Submit(context.Context, []llm.CompleteRequest) (string, error) {
	if b.calls.Add(1) == 1 {
		close(b.started)
		<-b.release
	}
	return "blocked-batch", nil
}
func (*blockingEditionBatch) Poll(context.Context, string, []llm.CompleteRequest) ([]string, bool, error) {
	return nil, false, nil
}
func (*blockingEditionBatch) Cancel(context.Context, string) error { return nil }

func TestEditionWorkerDoesNotOverlapExpiredLease(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.ingest(t, "Beacon.")
	if _, err := f.db.Pool().Exec(context.Background(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	batch := &blockingEditionBatch{started: make(chan struct{}), release: make(chan struct{})}
	ctx = pipeline.WithEditionBatch(ctx, batch)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionDaily, f.s.now().Add(time.Hour), "overlap"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.s.RunEditionJobs(ctx, 1); done <- err }()
	<-batch.started
	f.clock.Add(int64(editionJobLease + time.Minute))
	processed, err := f.s.RunEditionJobs(ctx, 1)
	close(batch.release)
	firstErr := <-done
	if err != nil || firstErr != nil || processed != 0 || batch.calls.Load() != 1 {
		t.Fatalf("overlap processed=%d calls=%d errors=%v/%v", processed, batch.calls.Load(), err, firstErr)
	}
}

type pollingErrorBatch struct{ testEditionBatch }

func (b *pollingErrorBatch) Poll(context.Context, string, []llm.CompleteRequest) ([]string, bool, error) {
	b.poll++
	if !b.ready {
		return nil, false, context.DeadlineExceeded
	}
	return []string{b.output}, true, nil
}

func TestEditionTransientPollKeepsBatch(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "Beacon.")
	if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	output := groundedOutput(id, coreHash("bodyBeacon."), "Beacon.", "Beacon.")
	p := &generatedProvider{outputs: map[string]string{"edition": output}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	batch := &pollingErrorBatch{testEditionBatch: testEditionBatch{output: output}}
	ctx = pipeline.WithEditionBatch(ctx, batch)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, f.s.now().Add(time.Hour), "poll-timeout"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		f.clock.Add(int64(time.Minute))
		if n, err := f.s.RunEditionJobs(ctx, 1); err != nil || n != 1 {
			t.Fatalf("poll jobs=%d err=%v", n, err)
		}
		var saved string
		var attempts int
		if err := f.db.Pool().QueryRow(ctx, `SELECT coalesce(payload->>'batch_id',''),attempts FROM jobs WHERE kind='edition'`).Scan(&saved, &attempts); err != nil {
			t.Fatal(err)
		}
		if batch.cancel != 0 || saved != "batch-one" || attempts != 0 {
			t.Fatalf("transient poll: cancellations=%d handle=%q attempts=%d", batch.cancel, saved, attempts)
		}
	}
	batch.ready = true
	f.clock.Add(int64(time.Minute))
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	latest, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || latest.Status != "ready" || len(p.payloads()) != 0 || batch.poll != 4 {
		t.Fatalf("collection status=%s calls=%d polls=%d err=%v", latest.Status, len(p.payloads()), batch.poll, err)
	}
}

func TestEditionChangedSourcesCancellationFailureFallsBack(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	keep := f.ingest(t, "Current consenting article.")
	remove := f.ingest(t, "Removed article.")
	if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(keep, coreHash("bodyCurrent consenting article."), "Current article.", "Current consenting article.")}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	batch := &cancellingBatch{}
	batch.ready = true
	batch.output = groundedOutput(remove, coreHash("bodyRemoved article."), "Stale result.", "Removed article.")
	ctx = pipeline.WithEditionBatch(ctx, batch)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, f.s.now().Add(time.Hour), "changed-cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE entries SET deleted_at=now() WHERE id=$1`, remove); err != nil {
		t.Fatal(err)
	}
	// Cancellation fails before the deadline too; it must not consume failure attempts.
	f.clock.Add(int64(time.Minute))
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(time.Hour))
	for range 3 {
		if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
			t.Fatal(err)
		}
		f.clock.Add(int64(time.Minute))
	}
	latest, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || latest.Status != "ready" || !strings.Contains(latest.Error, "cancellation") || len(p.payloads()) != 1 || batch.poll != 0 {
		t.Fatalf("changed fallback: status=%s warning=%q calls=%d polls=%d err=%v", latest.Status, latest.Error, len(p.payloads()), batch.poll, err)
	}
	if len(latest.Sections[0].Sources) != 1 || latest.Sections[0].Sources[0].EntryID != keep {
		t.Fatal("stale sources reused", latest.Sections[0].Sources)
	}
}

func TestEditionRetryReusesCompletedSections(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	secondFeed := entriesTestFeed(t, f.db, "https://second.test/edition", 0)
	entriesTestSub(t, f.db, f.a.UserID, secondFeed, "week")
	if _, err := f.s.RecordFetch(t.Context(), FetchOutcome{FeedID: secondFeed, Status: FetchOK, Items: []IngestItem{coreItem("Second article.")}}); err != nil {
		t.Fatal(err)
	}
	var second int64
	if err := f.db.Pool().QueryRow(t.Context(), `SELECT id FROM entries WHERE feed_id=$1`, secondFeed).Scan(&second); err != nil {
		t.Fatal(err)
	}
	first := f.ingest(t, "First article.")
	var folder int64
	if err := f.db.Pool().QueryRow(t.Context(), `INSERT INTO folders(user_id,name) VALUES($1,'A first section') RETURNING id`, f.a.UserID).Scan(&folder); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week',folder_id=$2 WHERE id=$1`, f.subscription, folder); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(first, coreHash("bodyFirst article."), "First claim.", "First article.")}}
	// One full reservation fits; its actual usage leaves too little for another.
	req, err := pipeline.GroundedRequest(llm.TaskEdition, "This week", []pipeline.Source{{EntryID: first, ContentHash: coreHash("bodyFirst article."), Text: "First article."}})
	if err != nil {
		t.Fatal(err)
	}
	limit, err := llm.EstimateCompletion(req, llm.ModelInfo{InputPer1M: 1000000, OutputPer1M: 1000000})
	if err != nil {
		t.Fatal(err)
	}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, limit)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, time.Time{}, "section-retry"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	paused, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || paused.Status != "paused" || len(p.payloads()) != 1 || len(paused.Sections[0].Claims) == 0 {
		t.Fatalf("pause: status=%s calls=%d err=%v", paused.Status, len(p.payloads()), err)
	}
	p.set("edition", groundedOutput(second, coreHash("bodySecond article."), "Second claim.", "Second article."))
	ctx = generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 10000)
	f.clock.Add(int64(time.Minute))
	if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	latest, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || latest.Status != "ready" || len(p.payloads()) != 2 {
		t.Fatalf("resume: status=%s total_provider_calls=%d want=2 err=%v", latest.Status, len(p.payloads()), err)
	}
	if latest.Sections[0].Claims[0].Text != "First claim." || latest.Sections[1].Claims[0].Text != "Second claim." {
		t.Fatal("section progress not preserved", latest.Sections)
	}
}

func TestEditionCompletedSectionInvalidation(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, change := range []string{"model", "prompt", "consent", "deleted source"} {
		t.Run(change, func(t *testing.T) {
			f := newCoreFixture(t, template)
			feed := f.feed
			var id int64
			if change == "consent" {
				feed = entriesTestFeed(t, f.db, "https://private.test/progress", f.a.UserID)
				entriesTestSub(t, f.db, f.a.UserID, feed, "week")
				if _, err := f.db.Pool().Exec(t.Context(), `UPDATE feeds SET llm_allowed=true WHERE id=$1`, feed); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.RecordFetch(t.Context(), FetchOutcome{FeedID: feed, Status: FetchOK, Items: []IngestItem{coreItem("Beacon.")}}); err != nil {
					t.Fatal(err)
				}
				if err := f.db.Pool().QueryRow(t.Context(), `SELECT id FROM entries WHERE feed_id=$1`, feed).Scan(&id); err != nil {
					t.Fatal(err)
				}
			} else {
				id = f.ingest(t, "Beacon.")
			}
			if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
				t.Fatal(err)
			}
			p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("bodyBeacon."), "Old prose.", "Beacon.")}}
			ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
			out, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, time.Time{}, "invalidate-section")
			if err != nil {
				t.Fatal(err)
			}
			job, err := f.s.claimEdition(ctx)
			if err != nil || job == nil {
				t.Fatal(job, err)
			}
			if err := f.s.generateEdition(ctx, job, &out); err != nil {
				t.Fatal(err)
			}
			p.set("edition", groundedOutput(id, coreHash("bodyBeacon."), "New prose.", "Beacon."))
			var other *pipelineProvider
			switch change {
			case "model":
				other = &pipelineProvider{classifier: groundedOutput(id, coreHash("bodyBeacon."), "New prose.", "Beacon.")}
				ctx = pipelineTestContext(t, f.db, other, []llm.Task{llm.TaskEdition}, 0)
			case "prompt":
				// Recreate persisted progress from a worker using an older prompt.
				out.PromptVersion = "edition-old"
				data, err := json.Marshal(struct {
					Model, PromptVersion string
					Sources              []pipeline.Source
				}{out.Model, out.PromptVersion, out.Sections[0].Sources})
				if err != nil {
					t.Fatal(err)
				}
				job.job.CompletedSections[0] = fmt.Sprintf("%x", sha256.Sum256(data))
			case "consent":
				if _, err := f.db.Pool().Exec(ctx, `UPDATE feeds SET llm_allowed=false WHERE id=$1`, feed); err != nil {
					t.Fatal(err)
				}
			case "deleted source":
				if _, err := f.db.Pool().Exec(ctx, `UPDATE entries SET deleted_at=now() WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.s.generateEdition(ctx, job, &out); err != nil {
				t.Fatal(err)
			}
			calls := len(p.payloads())
			if other != nil {
				calls += len(other.requests())
			}
			if change == "consent" || change == "deleted source" {
				if calls != 1 || len(out.Sections[0].Claims) != 0 || len(out.Sections[0].Sources) != 0 {
					t.Fatalf("revoked prose reused: calls=%d section=%+v", calls, out.Sections[0])
				}
				return
			}
			if calls != 2 || out.Sections[0].Claims[0].Text != "New prose." || out.PromptVersion != pipeline.EditionPromptVersion {
				t.Fatalf("changed %s reused prose: calls=%d edition=%+v", change, calls, out)
			}
		})
	}
}
