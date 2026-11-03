package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestRunContinuesAfterRoutineFailure(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/limited" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Healthy</title></channel></rss>`)
	}))
	defer server.Close()
	host, port := serverHostPort(t, server)
	f, _ := integrationFetcher(t, db, testClient(t, host), port)
	f.config.Workers = 1
	f.config.BatchSize = 2
	limited := insertFeed(t, db, server.URL+"/limited")
	healthy := insertFeed(t, db, server.URL+"/healthy")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var user int64
	if err := db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('reader','$argon2id$test') RETURNING id").Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2),($1,$3)", user, limited, healthy); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE feeds SET next_fetch_at=$1", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	finished := false
	defer func() {
		cancel()
		if finished {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("fetcher did not stop")
		}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var success bool
		if err := db.Pool().QueryRow(ctx, "SELECT last_success_at IS NOT NULL FROM feeds WHERE id=$1", healthy).Scan(&success); err != nil {
			t.Fatal(err)
		}
		if success {
			break
		}
		select {
		case err := <-done:
			finished = true
			t.Fatalf("fetcher stopped after routine failure: %v", err)
		case <-ctx.Done():
			t.Fatal("healthy feed was not fetched:", ctx.Err())
		case <-ticker.C:
		}
	}
	if class, _ := persistedClass(t, db, limited); class != "rate_limit" {
		t.Fatalf("limited feed class %q", class)
	}
	cancel()
	err := <-done
	finished = true
	if err != nil {
		t.Fatalf("fetcher returned an error: %v", err)
	}
}
