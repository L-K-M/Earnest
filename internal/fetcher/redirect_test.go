package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestFragmentRedirectOutcomePersisted(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name        string
		redirect    int
		status      int
		body, class string
	}{
		{"success", http.StatusMovedPermanently, http.StatusOK, `<rss version="2.0"><channel><title>News</title><item><guid>one</guid><title>Item</title><description>Body</description></item></channel></rss>`, ""},
		{"not_modified", http.StatusPermanentRedirect, http.StatusNotModified, "", ""},
		{"http_failure", http.StatusMovedPermanently, http.StatusInternalServerError, "", "http"},
		{"parse_failure", http.StatusPermanentRedirect, http.StatusOK, "<rss><channel>", "parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/old" {
					http.Redirect(w, r, "/feed?edition=1#part%2Fanchor", tc.redirect)
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			db := template.New(t)
			host, port := serverHostPort(t, server)
			f, _ := integrationFetcher(t, db, testClient(t, host), port)
			id := insertFeed(t, db, server.URL+"/old")
			if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: id, URL: server.URL + "/old"}); err != nil {
				t.Fatalf("one feed's redirect must not stop the fetcher: %v", err)
			}
			var target string
			if err := db.Pool().QueryRow(context.Background(), "SELECT permanent_redirect_url FROM feeds WHERE id=$1", id).Scan(&target); err != nil {
				t.Fatal(err)
			}
			if want := server.URL + "/feed?edition=1"; target != want {
				t.Fatalf("redirect target = %q, want %q", target, want)
			}
			if class, _ := persistedClass(t, db, id); class != tc.class {
				t.Fatalf("persisted class = %q, want %q", class, tc.class)
			}
		})
	}
}

func TestRejectedRedirectedItemsKeepNormalizedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/feed#part%2Fanchor", http.StatusMovedPermanently)
			return
		}
		fmt.Fprint(w, `<rss version="2.0"><channel><title>News</title></channel></rss>`)
	}))
	defer server.Close()
	f, svc := testFetcher(t, server)
	svc.rejects = []error{service.ErrInvalid}
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: server.URL + "/old"}); err != nil {
		t.Fatal(err)
	}
	if len(svc.outcomes) != 2 || svc.outcomes[1].Status != service.FetchFailed {
		t.Fatalf("expected a recorded rejection fallback, got %+v", svc.outcomes)
	}
	for _, outcome := range svc.outcomes {
		if want := server.URL + "/feed"; outcome.PermanentRedirectURL != want {
			t.Fatalf("redirect target = %q, want %q", outcome.PermanentRedirectURL, want)
		}
	}
}
