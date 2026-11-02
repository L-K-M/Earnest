package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/api"
	"github.com/L-K-M/Earnest/internal/metrics"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

type fakeProber struct {
	called bool
	url    string
	meta   service.FeedMeta
}

func (f *fakeProber) Probe(context.Context, string, *service.FeedCredentials) (string, service.FeedMeta, []service.IngestItem, error) {
	f.called = true
	return f.url, f.meta, nil, nil
}

func TestFeedProberForwards(t *testing.T) {
	p := &feedProber{}
	if _, _, _, err := p.Probe(context.Background(), "https://example.com", nil); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("unassigned probe error = %v, want ErrUnavailable", err)
	}
	p.delegate = &fakeProber{url: "https://example.com/feed.xml", meta: service.FeedMeta{Title: "Example"}}
	rawURL, meta, _, err := p.Probe(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rawURL != "https://example.com/feed.xml" || meta.Title != "Example" {
		t.Fatalf("forwarded probe = %q %+v", rawURL, meta)
	}
}

func TestGreaderFallbackUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	newGreader(nil, nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/greader/reader/api/0/stream/contents", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestPrivatePorts(t *testing.T) {
	got := privatePorts([]string{"ollama:11434", "ntfy:80", "not-a-hostport"})
	if !reflect.DeepEqual(got, []uint16{11434, 80}) {
		t.Fatalf("ports = %v", got)
	}
	if ports := privatePorts(nil); len(ports) != 0 {
		t.Fatalf("ports = %v, want none", ports)
	}
}

func TestRetentionStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		// A cancelled context must return before the first sweep, so a nil
		// service is never dereferenced.
		runRetention(ctx, nil, time.Hour, slog.Default())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("retention loop did not stop")
	}
}

func TestOpenAPICommand(t *testing.T) {
	var buf bytes.Buffer
	if err := openapi(&buf); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("openapi output is not valid JSON: %.200s", buf.Bytes())
	}
	var doc struct {
		OpenAPI string `json:"openapi"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil || doc.OpenAPI == "" {
		t.Fatalf("not an OpenAPI document: %v", err)
	}
}

// TestBuildMux needs a real service because the API layer takes concrete
// types; it skips without EARNEST_TEST_DATABASE_URL like every store test.
func TestBuildMux(t *testing.T) {
	template := storetest.NewTemplate(t)
	db := template.New(t)
	secret := bytes.Repeat([]byte{7}, 32)
	svc, err := service.New(db, service.Deps{FeedProber: &feedProber{}, SecretKey: secret})
	if err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuth(db, time.Now, secret)
	handler := api.SecurityHeaders(buildMux("http://localhost:8080", svc, auth, slog.Default(), metrics.New()))

	for _, tc := range []struct {
		path       string
		wantStatus int
	}{
		{"/healthz", http.StatusOK},
		{"/metrics", http.StatusOK},
		{"/openapi.json", http.StatusOK},
		{"/api/greader/reader/api/0/stream/contents", http.StatusServiceUnavailable},
		{"/", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.wantStatus {
			t.Errorf("GET %s: status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s: no Content-Security-Policy header", tc.path)
		}
	}
}
