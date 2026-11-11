package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestWebhookRedirectDoesNotRepeatProcessedPost(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, status := range []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var posts, retrievals atomic.Int64
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					retrievals.Add(1)
					w.WriteHeader(http.StatusOK)
					return
				}
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				// A receiver can process the POST before referring to its result.
				posts.Add(1)
				w.Header().Set("Location", "/receipt")
				w.WriteHeader(status)
			}))
			t.Cleanup(receiver.Close)
			u, err := url.Parse(receiver.URL)
			if err != nil {
				t.Fatal(err)
			}
			client, err := outbound.New(outbound.Config{
				Version:    "webhook-redirect-test",
				Spacing:    time.Millisecond,
				AllowHosts: []string{u.Host},
			})
			if err != nil {
				t.Fatal(err)
			}
			f := newWebhookFixture(t, template, client)
			ctx := context.Background()
			hook, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{
				URL: receiver.URL, Events: []string{string(WebhookEntryMatched)},
			}, "see-other")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{
				Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7,
			}); err != nil {
				t.Fatal(err)
			}
			if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 1 {
				t.Fatalf("first attempt: processed=%d err=%v", processed, err)
			}
			f.clock.Add(int64(2 * time.Hour))
			if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 0 {
				t.Errorf("ambiguous POST must not be retried: processed=%d err=%v", processed, err)
			}
			if got := posts.Load(); got != 1 {
				t.Errorf("receiver processed %d POSTs; want exactly one", got)
			}
			if got := retrievals.Load(); got != 0 {
				t.Errorf("followed the Location %d times", got)
			}
			if got := jobCount(t, f); got != 0 {
				t.Errorf("ambiguous outcome left %d retry jobs", got)
			}
			if failures, active := webhookFailures(t, f, hook.ID); failures != 0 || !active {
				t.Errorf("ambiguous response counted as refusal: failures=%d active=%v", failures, active)
			}
		})
	}
}
