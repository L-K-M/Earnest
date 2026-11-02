package service

import (
	"context"
	"errors"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"io"
	"net"
	"testing"

	"github.com/L-K-M/Earnest/internal/outbound"
)

func TestWebhookReadFailureIsUncertain(t *testing.T) {
	err := &outbound.Error{Class: outbound.Connect, Err: &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}}
	if got := classifyWebhook(nil, err); got.outcome != outcomeAmbiguous {
		t.Fatalf("read failure after transmission must never retry: %+v", got)
	}
}

func TestWebhookCompletionRejectsRestoredGeneration(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	hook, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://hooks.example/events", Events: []string{string(WebhookFeedFailing)}}, "hook")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookFeedFailing, FeedID: f.feed}); err != nil {
		t.Fatal(err)
	}
	job, due, err := f.s.claimWebhookJob(ctx)
	if err != nil || !due || job == nil {
		t.Fatal(job, due, err)
	}
	if _, err := f.db.RotateGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	err = f.s.resolveWebhookJob(ctx, job.jobID, hook.ID, job.attempts, webhookResult{outcome: outcomeDefinite, class: "rejected"}, job.generation)
	if !errors.Is(err, ErrGone) {
		t.Fatal("old completion changed restored jobs", err)
	}
}
