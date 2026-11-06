package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestSubscribeProbeTimeout(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	old := probeTimeout
	probeTimeout = 100 * time.Millisecond
	defer func() { probeTimeout = old }()
	f.s.prober = &subscriptionsProber{probe: func(ctx context.Context, _ string, _ *FeedCredentials) (string, FeedMeta, []IngestItem, error) {
		<-ctx.Done()
		return "", FeedMeta{}, nil, ctx.Err()
	}}
	start := time.Now()
	_, err := f.s.Subscribe(context.Background(), f.a, "https://slow.test/feed", SubscribeOptions{OpID: "probe-timeout"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected the timeout class in the error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("probe was not bounded: %v", elapsed)
	}
	// The bounded probe must not have persisted a feed or subscription.
	var n int
	if err := f.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM subscriptions s JOIN feeds f ON f.id=s.feed_id WHERE s.user_id=$1 AND f.url='https://slow.test/feed'`, f.a.UserID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("subscription persisted after failed probe: %d %v", n, err)
	}
}
