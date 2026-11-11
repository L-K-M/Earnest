package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// webhookServer records every delivery attempt for assertions.
type webhookServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []webhookRequest
	status   int
}

type webhookRequest struct {
	id, timestamp, signature string
	body                     []byte
}

func newWebhookServer(t *testing.T, status int) *webhookServer {
	t.Helper()
	s := &webhookServer{status: status}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		s.mu.Lock()
		s.requests = append(s.requests, webhookRequest{
			id:        r.Header.Get("webhook-id"),
			timestamp: r.Header.Get("webhook-timestamp"),
			signature: r.Header.Get("webhook-signature"),
			body:      body,
		})
		s.mu.Unlock()
		w.WriteHeader(s.status)
	}))
	t.Cleanup(s.Server.Close)
	return s
}

func (s *webhookServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *webhookServer) last() webhookRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[len(s.requests)-1]
}

// webhookClient builds a real outbound client that permits the loopback test
// server through the SSRF policy, exercising the same checks as production.
func webhookClient(t *testing.T, s *webhookServer) *outbound.Client {
	t.Helper()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := outbound.New(outbound.Config{
		Version:    "webhook-test",
		Spacing:    time.Millisecond,
		AllowHosts: []string{u.Host},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func newWebhookFixture(t *testing.T, template *storetest.Template, client *outbound.Client) *coreFixture {
	t.Helper()
	f := newCoreFixture(t, template)
	if client != nil {
		svc, err := New(f.db, Deps{Clock: f.s.now, SecretKey: []byte(strings.Repeat("s", 32)), Outbound: client})
		if err != nil {
			t.Fatal(err)
		}
		f.s = svc
	}
	return f
}

func jobCount(t *testing.T, f *coreFixture) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM jobs WHERE kind='webhook'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func jobRunAt(t *testing.T, f *coreFixture) (time.Time, int, *string) {
	t.Helper()
	var runAt time.Time
	var attempts int
	var lastErr *string
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT run_at,attempts,last_error FROM jobs WHERE kind='webhook'").Scan(&runAt, &attempts, &lastErr); err != nil {
		t.Fatal(err)
	}
	return runAt, attempts, lastErr
}

func webhookFailures(t *testing.T, f *coreFixture, id int64) (int, bool) {
	t.Helper()
	var n int
	var active bool
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT consecutive_failures,active FROM webhooks WHERE id=$1", id).Scan(&n, &active); err != nil {
		t.Fatal(err)
	}
	return n, active
}

func TestWebhookEnqueue(t *testing.T) {
	template := storetest.NewTemplate(t)
	ctx := context.Background()
	t.Run("matching_dedupe_and_generation", func(t *testing.T) {
		f := newWebhookFixture(t, template, nil)
		one, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://a.example.test/", Events: []string{"entry.matched"}}, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://b.example.test/", Events: []string{"feed.failing"}}, "w2"); err != nil {
			t.Fatal(err)
		}
		ev := WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 44}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, ev); err != nil {
			t.Fatal(err)
		}
		if n := jobCount(t, f); n != 1 {
			t.Fatalf("only subscribed webhooks get jobs: %d", n)
		}
		var payload []byte
		if err := f.db.Pool().QueryRow(ctx, "SELECT payload FROM jobs").Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var job webhookJob
		if err := json.Unmarshal(payload, &job); err != nil {
			t.Fatal(err)
		}
		generation, err := f.db.Generation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job.WebhookID != one.ID || job.Generation != generation || job.Event != "entry.matched" || !strings.HasPrefix(job.MessageID, "msg_") {
			t.Fatalf("job payload: %+v", job)
		}
		// Duplicate emission while pending is deduplicated by unique_key.
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, ev); err != nil {
			t.Fatal(err)
		}
		if n := jobCount(t, f); n != 1 {
			t.Fatalf("dedupe: %d", n)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookFeedFailing, FeedID: f.feed, EntryID: 9}); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad event emitted", err)
		}
	})
	t.Run("feed_failing_fans_out_to_subscribers", func(t *testing.T) {
		f := newWebhookFixture(t, template, nil)
		if _, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://a.example.test/", Events: []string{"feed.failing"}}, "w1"); err != nil {
			t.Fatal(err)
		}
		var other, principal int64
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('other','$argon2id$test') RETURNING id").Scan(&other); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id", other).Scan(&principal); err != nil {
			t.Fatal(err)
		}
		// An unsubscribed user's webhook hears nothing about this feed.
		if _, err := f.db.Pool().Exec(ctx, "INSERT INTO webhooks(user_id,created_by,url,secret_encrypted,events) VALUES($1,$2,'https://b.example.test/','x',ARRAY['feed.failing'])", other, principal); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitFeedFailing(ctx, f.feed); err != nil {
			t.Fatal(err)
		}
		if n := jobCount(t, f); n != 1 {
			t.Fatalf("subscriber fan-out: %d", n)
		}
		// Once the other user subscribes, both webhooks are notified.
		if _, err := f.db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", other, f.feed); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "DELETE FROM jobs"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitFeedFailing(ctx, f.feed); err != nil {
			t.Fatal(err)
		}
		if n := jobCount(t, f); n != 2 {
			t.Fatalf("two subscribers: %d", n)
		}
	})
}

func TestWebhookClassification(t *testing.T) {
	for _, tc := range []struct {
		class    outbound.Class
		definite bool
	}{
		{outbound.DeniedAddress, true}, {outbound.DNS, true}, {outbound.Connect, false},
		{outbound.TLS, true}, {outbound.HTTPGone, true}, {outbound.HTTP403, true},
		{outbound.HTTP429, true}, {outbound.RedirectLoop, true},
		{outbound.Timeout, false}, {outbound.HTTP5xx, false}, {outbound.TooLarge, false},
		{outbound.Decode, false}, {"", false},
	} {
		if got := webhookDefinite(tc.class); got != tc.definite {
			t.Fatalf("class %s: %v", tc.class, got)
		}
	}
	if r := classifyWebhook(&outbound.Result{Response: &http.Response{StatusCode: 200}}, nil); r.outcome != outcomeDelivered {
		t.Fatal("2xx")
	}
	if r := classifyWebhook(&outbound.Result{Response: &http.Response{StatusCode: http.StatusFound}}, nil); r.outcome != outcomeDefinite {
		t.Fatal("302 remains a definite refusal when redirects are disabled")
	}
	if r := classifyWebhook(nil, &outbound.Error{Class: outbound.HTTP5xx}); r.outcome != outcomeAmbiguous {
		t.Fatal("5xx is ambiguous")
	}
	if r := classifyWebhook(nil, &outbound.Error{Class: outbound.HTTP429, RetryAfter: 2 * time.Minute}); r.outcome != outcomeDefinite || r.retryAfter != 2*time.Minute {
		t.Fatal("429 carries Retry-After")
	}
	// Only a refused dial proves the POST never left; a later read or write
	// failure may have already transmitted it.
	if r := classifyWebhook(nil, &outbound.Error{Class: outbound.Connect, Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}}); r.outcome != outcomeDefinite {
		t.Fatal("refused dial is definite")
	}
	if r := classifyWebhook(nil, &outbound.Error{Class: outbound.Connect, Err: &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}}); r.outcome != outcomeAmbiguous {
		t.Fatal("read after transmission is ambiguous")
	}
	if r := classifyWebhook(nil, &outbound.Error{Class: outbound.Connect}); r.outcome != outcomeAmbiguous {
		t.Fatal("unattributed connect failure is ambiguous")
	}
	for _, tc := range []struct {
		attempts   int
		retryAfter time.Duration
		want       time.Duration
	}{
		{1, 0, webhookBaseBackoff},
		{2, 0, 2 * webhookBaseBackoff},
		{9, 0, webhookMaxBackoff},
		{1, 45 * time.Second, 45 * time.Second},
		{1, 48 * time.Hour, webhookMaxBackoff},
	} {
		if got := webhookBackoff(tc.attempts, tc.retryAfter); got != tc.want {
			t.Fatalf("backoff(%d,%v): %v want %v", tc.attempts, tc.retryAfter, got, tc.want)
		}
	}
}

func TestWebhookDelivery(t *testing.T) {
	template := storetest.NewTemplate(t)
	ctx := context.Background()
	t.Run("signed_post_and_reset", func(t *testing.T) {
		server := newWebhookServer(t, 200)
		f := newWebhookFixture(t, template, webhookClient(t, server))
		created, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: server.URL, Events: []string{"entry.matched"}}, "w")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 44}); err != nil {
			t.Fatal(err)
		}
		processed, err := f.s.RunWebhookJobs(ctx, 10)
		if err != nil || processed != 1 || server.count() != 1 {
			t.Fatalf("run: %d %v requests=%d", processed, err, server.count())
		}
		if n := jobCount(t, f); n != 0 {
			t.Fatalf("delivered job must be gone: %d", n)
		}
		req := server.last()
		if !strings.HasPrefix(req.id, "msg_") {
			t.Fatalf("webhook-id: %q", req.id)
		}
		stamp, err := strconv.ParseInt(req.timestamp, 10, 64)
		if err != nil || stamp != f.s.now().Unix() {
			t.Fatalf("webhook-timestamp: %q %v", req.timestamp, err)
		}
		key, err := decodeWebhookSecret(created.Secret)
		if err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(req.id + "." + req.timestamp + "."))
		mac.Write(req.body)
		if req.signature != "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("webhook-signature does not verify: %q", req.signature)
		}
		var body map[string]any
		if err := json.Unmarshal(req.body, &body); err != nil {
			t.Fatal(err)
		}
		if body["type"] != "entry.matched" || body["timestamp"] == nil {
			t.Fatalf("body: %s", req.body)
		}
		data, _ := body["data"].(map[string]any)
		for k := range data {
			if !slices.Contains([]string{"feed_id", "entry_id", "edition_id"}, k) {
				t.Fatalf("payload carries %s: %s", k, req.body)
			}
		}
		if data["feed_id"] != float64(f.feed) || data["entry_id"] != float64(44) {
			t.Fatalf("payload ids: %s", req.body)
		}
	})
	t.Run("definite_failures_disable_after_five", func(t *testing.T) {
		server := newWebhookServer(t, 404)
		f := newWebhookFixture(t, template, webhookClient(t, server))
		created, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: server.URL, Events: []string{"entry.matched"}}, "w")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= webhookMaxFailures; i++ {
			f.clock.Add(int64(2 * time.Hour))
			processed, err := f.s.RunWebhookJobs(ctx, 10)
			if err != nil || processed != 1 {
				t.Fatalf("attempt %d: %d %v", i, processed, err)
			}
			failures, active := webhookFailures(t, f, created.ID)
			if failures != i || active != (i < webhookMaxFailures) {
				t.Fatalf("attempt %d: failures=%d active=%v", i, failures, active)
			}
		}
		if server.count() != webhookMaxFailures {
			t.Fatalf("requests: %d", server.count())
		}
		if n := jobCount(t, f); n != 0 {
			t.Fatalf("disabled webhook must drop pending jobs: %d", n)
		}
	})
	t.Run("backoff_schedule_after_definite", func(t *testing.T) {
		server := newWebhookServer(t, 403)
		f := newWebhookFixture(t, template, webhookClient(t, server))
		created, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: server.URL, Events: []string{"entry.matched"}}, "w")
		if err != nil {
			t.Fatal(err)
		}
		before := f.s.now()
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.RunWebhookJobs(ctx, 10); err != nil {
			t.Fatal(err)
		}
		runAt, attempts, lastErr := jobRunAt(t, f)
		if attempts != 1 || lastErr == nil || *lastErr != "http_403" {
			t.Fatalf("reschedule: attempts=%d last=%v", attempts, lastErr)
		}
		if runAt.Before(before.Add(webhookBaseBackoff)) {
			t.Fatalf("run_at lacks backoff: %v < %v", runAt, before)
		}
		if failures, _ := webhookFailures(t, f, created.ID); failures != 1 {
			t.Fatalf("failures: %d", failures)
		}
		// The job must not run again until its backoff expires.
		if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 0 {
			t.Fatalf("early retry: %d %v", processed, err)
		}
		f.clock.Add(int64(webhookBaseBackoff))
		if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 1 {
			t.Fatalf("due retry: %d %v", processed, err)
		}
	})
	t.Run("ambiguous_5xx_never_retried", func(t *testing.T) {
		server := newWebhookServer(t, 500)
		f := newWebhookFixture(t, template, webhookClient(t, server))
		created, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: server.URL, Events: []string{"entry.matched"}}, "w")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 1 || server.count() != 1 {
			t.Fatalf("run: %d %v", processed, err)
		}
		if n := jobCount(t, f); n != 0 {
			t.Fatalf("ambiguous outcome must not be retried: %d", n)
		}
		if failures, _ := webhookFailures(t, f, created.ID); failures != 0 {
			t.Fatalf("ambiguous failure must not count: %d", failures)
		}
	})
	t.Run("unresolved_attempt_and_restore_never_replay", func(t *testing.T) {
		server := newWebhookServer(t, 200)
		f := newWebhookFixture(t, template, webhookClient(t, server))
		if _, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: server.URL, Events: []string{"entry.matched"}}, "w"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		// A crash between the committed attempt and the send leaves the row
		// attempted with no resolution; it must be dropped, never resent.
		if _, err := f.db.Pool().Exec(ctx, "UPDATE jobs SET attempts=1,last_error=NULL"); err != nil {
			t.Fatal(err)
		}
		if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 1 {
			t.Fatalf("crash drop: %d %v", processed, err)
		}
		if jobCount(t, f) != 0 || server.count() != 0 {
			t.Fatalf("unresolved attempt replayed: jobs=%d requests=%d", jobCount(t, f), server.count())
		}
		// A resolved definite failure is the only state allowed to send again.
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE jobs SET attempts=1,last_error='http_403'"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.RunWebhookJobs(ctx, 10); err != nil || server.count() != 1 {
			t.Fatalf("sanctioned retry: %v requests=%d", err, server.count())
		}
		// After a restore every surviving job belongs to an old generation.
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.RotateGeneration(ctx); err != nil {
			t.Fatal(err)
		}
		if processed, err := f.s.RunWebhookJobs(ctx, 10); err != nil || processed != 1 {
			t.Fatalf("restore drop: %d %v", processed, err)
		}
		if jobCount(t, f) != 0 || server.count() != 1 {
			t.Fatalf("restored job replayed: jobs=%d requests=%d", jobCount(t, f), server.count())
		}
	})
	t.Run("inactive_and_unsubscribed_jobs_drop", func(t *testing.T) {
		server := newWebhookServer(t, 200)
		f := newWebhookFixture(t, template, webhookClient(t, server))
		created, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: server.URL, Events: []string{"entry.matched", "feed.failing"}}, "w")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookEntryMatched, FeedID: f.feed, EntryID: 7}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.UpdateWebhook(ctx, f.a, created.ID, WebhookPatch{Events: []string{"feed.failing"}}, "ev"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookFeedFailing, FeedID: f.feed}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE webhooks SET active=false WHERE id=$1", created.ID); err != nil {
			t.Fatal(err)
		}
		processed, err := f.s.RunWebhookJobs(ctx, 10)
		if err != nil || processed != 2 || server.count() != 0 {
			t.Fatalf("drops: %d %v requests=%d", processed, err, server.count())
		}
	})
}
