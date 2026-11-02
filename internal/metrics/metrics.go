// Package metrics provides a small Prometheus text endpoint.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry stores fetcher measurements safely across workers.
type Registry struct {
	mu       sync.Mutex
	fetches  map[string]uint64
	items    uint64
	queue    int
	buckets  [7]uint64
	duration float64
	count    uint64
}

var bounds = [...]float64{0.1, 0.5, 1, 2, 5, 10}

// New returns an empty registry.
func New() *Registry { return &Registry{fetches: make(map[string]uint64)} }

// ObserveFetch increments the result counter and duration histogram in seconds.
func (r *Registry) ObserveFetch(class string, seconds float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fetches[class]++
	r.count++
	r.duration += seconds
	for i, b := range bounds {
		if seconds <= b {
			r.buckets[i]++
		}
	}
	r.buckets[len(bounds)]++
}

// AddItems records the number of created and updated entries.
func (r *Registry) AddItems(n int) {
	if n < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items += uint64(n)
}

// SetQueueDepth sets the number of claimed feeds waiting for a worker.
func (r *Registry) SetQueueDepth(n int) { r.mu.Lock(); defer r.mu.Unlock(); r.queue = n }

// ServeHTTP writes Prometheus text exposition.
func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# TYPE earnest_fetches_total counter")
	keys := make([]string, 0, len(r.fetches))
	for k := range r.fetches {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		label := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(k)
		fmt.Fprintf(w, "earnest_fetches_total{class=\"%s\"} %d\n", label, r.fetches[k])
	}
	fmt.Fprintf(w, "# TYPE earnest_items_ingested_total counter\nearnest_items_ingested_total %d\n# TYPE earnest_scheduler_queue_depth gauge\nearnest_scheduler_queue_depth %d\n# TYPE earnest_fetch_duration_seconds histogram\n", r.items, r.queue)
	for i, b := range bounds {
		fmt.Fprintf(w, "earnest_fetch_duration_seconds_bucket{le=\"%s\"} %d\n", strconv.FormatFloat(b, 'g', -1, 64), r.buckets[i])
	}
	fmt.Fprintf(w, "earnest_fetch_duration_seconds_bucket{le=\"+Inf\"} %d\nearnest_fetch_duration_seconds_sum %g\nearnest_fetch_duration_seconds_count %d\n", r.buckets[len(bounds)], r.duration, r.count)
}
