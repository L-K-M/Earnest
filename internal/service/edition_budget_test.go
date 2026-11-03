package service

import (
	"context"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

type pausingEditionBatch struct {
	testEditionBatch
	paused bool
}

func (b *pausingEditionBatch) Submit(ctx context.Context, requests []llm.CompleteRequest) (string, error) {
	if b.paused {
		b.submit++
		return "", llm.ErrBudgetExhausted
	}
	return b.testEditionBatch.Submit(ctx, requests)
}

func TestEditionBatchBudgetPauseRetriesSubmission(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.ingest(t, "Weekly Beacon report.")
	if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	batch := &pausingEditionBatch{paused: true}
	ctx = pipeline.WithEditionBatch(ctx, batch)
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, f.s.now().Add(time.Hour), "paused-batch"); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.RunEditionJobs(ctx, 10); err != nil || n != 1 {
		t.Fatalf("pause: n=%d err=%v", n, err)
	}
	paused, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || paused.Status != "paused" || batch.submit != 1 {
		t.Fatalf("edition=%+v submit=%d err=%v", paused, batch.submit, err)
	}
	var attempts int
	if err := f.db.Pool().QueryRow(ctx, `SELECT attempts FROM jobs WHERE kind='edition'`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("budget consumed failure attempt: attempts=%d err=%v", attempts, err)
	}
	batch.paused = false
	f.clock.Add(int64(2 * time.Minute))
	if n, err := f.s.RunEditionJobs(ctx, 10); err != nil || n != 1 {
		t.Fatalf("resume: n=%d err=%v", n, err)
	}
	resumed, err := f.s.LatestEdition(ctx, f.a)
	if err != nil || resumed.Status != "pending" || batch.submit != 2 {
		t.Fatalf("edition=%+v submit=%d err=%v", resumed, batch.submit, err)
	}
}
