package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/service"
)

func TestSummaryExcerptProvenance(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	f, _ := testFetcher(t, server)
	for _, test := range []struct {
		name, full, summary string
		want                bool
	}{
		{"excerpt", "", "A short preview.", true},
		{"full", "The full text.", "A preview.", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			items, err := f.items(context.Background(), service.FeedToFetch{}, []feed.Item{{GUID: test.name, ContentHTML: test.full, SummaryHTML: test.summary}})
			if err != nil || len(items) != 1 || (items[0].ContentSource == service.ContentFromFeedExcerpt) != test.want {
				t.Fatalf("items %+v %v", items, err)
			}
		})
	}
}
