package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
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
