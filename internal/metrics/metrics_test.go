package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExposition(t *testing.T) {
	r := New()
	r.ObserveFetch("ok", 0.2)
	r.ObserveFetch("error", 2)
	r.AddItems(3)
	r.SetQueueDepth(4)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	for _, want := range []string{`earnest_fetches_total{class="ok"} 1`, `earnest_items_ingested_total 3`, `earnest_scheduler_queue_depth 4`, `earnest_fetch_duration_seconds_bucket{le="0.5"} 1`, `earnest_fetch_duration_seconds_count 2`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

type blockedWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return w.ResponseRecorder.Write(p)
}

func TestSlowScrapeDoesNotBlockMeasurements(t *testing.T) {
	r := New()
	r.ObserveFetch("ok", 0.2)
	r.AddItems(3)
	r.SetQueueDepth(4)
	w := &blockedWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
	served := make(chan struct{})
	go func() {
		r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
		close(served)
	}()
	<-w.entered
	updated := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(w.release) }) }
	defer func() {
		release()
		<-served
		<-updated
	}()
	go func() {
		r.ObserveFetch("new", 2)
		r.AddItems(1)
		r.SetQueueDepth(5)
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(5 * time.Second):
		t.Fatal("slow metrics response blocked fetch measurements")
	}
	release()
	<-served
	body := w.Body.String()
	for _, want := range []string{`earnest_fetches_total{class="ok"} 1`, `earnest_items_ingested_total 3`, `earnest_scheduler_queue_depth 4`, `earnest_fetch_duration_seconds_count 1`} {
		if !strings.Contains(body, want) {
			t.Errorf("snapshot missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `class="new"`) {
		t.Fatalf("scrape mixed measurements taken after writing began: %s", body)
	}
}
