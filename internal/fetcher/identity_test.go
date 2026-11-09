package fetcher

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
)

// commentsFeed is shaped like a WordPress comments feed: each comment's GUID
// is its post's URL plus a #comment fragment, and one comment is repeated.
const commentsFeed = `<rss version="2.0"><channel><title>Comments</title>
<item><title>Comment by Ann</title><link>https://example.org/post#comment-1</link><guid isPermaLink="false">https://example.org/?p=7#comment-1</guid><description>First</description></item>
<item><title>Comment by Bo</title><link>https://example.org/post#comment-2</link><guid isPermaLink="false">https://example.org/?p=7#comment-2</guid><description>Second</description></item>
<item><title>Comment by Ann</title><link>https://example.org/post#comment-1</link><guid isPermaLink="false">https://example.org/?p=7#comment-1</guid><description>First</description></item>
</channel></rss>`

func TestFetchKeepsCommentsOnOnePostApart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, commentsFeed)
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if len(s.outcomes) != 1 || s.outcomes[0].Status != service.FetchOK {
		t.Fatalf("outcomes = %+v", s.outcomes)
	}
	items := s.outcomes[0].Items
	// Two comments: the repeated one is sent once, and the fragment keeps
	// the two comments on post 7 apart.
	if len(items) != 2 || items[0].IdentityHash == items[1].IdentityHash {
		t.Fatalf("items = %+v", items)
	}
	// Both were stored under one identity by the earlier rule, which a fetch
	// adopts instead of duplicating.
	if items[0].LegacyIdentityHash == "" || items[0].LegacyIdentityHash != items[1].LegacyIdentityHash {
		t.Fatalf("legacy identities = %q, %q", items[0].LegacyIdentityHash, items[1].LegacyIdentityHash)
	}
}

func TestFetchRecordsRejectedItemsAsThatFeedsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", "v1")
		fmt.Fprint(w, commentsFeed)
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	s.rejects = []error{fmt.Errorf("duplicate feed identity: %w", service.ErrInvalid)}
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: server.URL}); err != nil {
		t.Fatalf("one feed's rejected data must not stop the fetcher: %v", err)
	}
	if len(s.outcomes) != 2 {
		t.Fatalf("outcomes = %+v", s.outcomes)
	}
	failed := s.outcomes[1]
	if failed.Status != service.FetchFailed || failed.ErrorClass != "parse" || len(failed.Items) != 0 || failed.ETag != "" || failed.NextFetchAt.IsZero() {
		t.Fatalf("failure record = %+v", failed)
	}
}

func TestFetchStillStopsOnOtherRecordErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, commentsFeed)
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	broken := errors.New("database unavailable")
	s.rejects = []error{broken}
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: server.URL}); !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the database error", err)
	}
	if len(s.outcomes) != 1 {
		t.Fatalf("a system failure must not be recorded as the feed's: %+v", s.outcomes)
	}
}
