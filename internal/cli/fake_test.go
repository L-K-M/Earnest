package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

const testToken = "earnest_app_testtoken"

type loggedRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
	auth   string
}

type requestLog struct {
	mu   sync.Mutex
	list []loggedRequest
}

func (l *requestLog) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	// Restore the body so the handler still sees it.
	r.Body = io.NopCloser(bytes.NewReader(body))
	l.mu.Lock()
	l.list = append(l.list, loggedRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.Query(),
		body:   body,
		auth:   r.Header.Get("Authorization"),
	})
	l.mu.Unlock()
}

func (l *requestLog) snapshot() []loggedRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]loggedRequest, len(l.list))
	copy(out, l.list)
	return out
}

// runStub starts a fake API and runs the client against it. The returned log
// records every request the client made.
func runStub(t *testing.T, handler http.HandlerFunc, token string, args ...string) (int, string, string, *requestLog) {
	t.Helper()
	log := &requestLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		handler(w, r)
	}))
	defer srv.Close()
	t.Setenv("EARNEST_URL", srv.URL)
	if token == "" {
		t.Setenv("EARNEST_TOKEN", "")
	} else {
		t.Setenv("EARNEST_TOKEN", token)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut)
	return code, out.String(), errOut.String(), log
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:      "/problems/" + code,
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		ErrorCode: code,
	})
}

func parseTestTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}

func timePtr(value string) *time.Time {
	t := parseTestTime(value)
	return &t
}

func decodeBody(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}

func requireCode(t *testing.T, code int, want int, errOut string) {
	t.Helper()
	if code != want {
		t.Fatalf("exit = %d, want %d; stderr: %s", code, want, errOut)
	}
}

// testSubscription is a valid Subscription wire object.
func testSubscription() subscription {
	return subscription{
		ID:      5,
		FeedID:  2,
		Title:   "Example Feed",
		URL:     "https://example.com/feed.xml",
		SiteURL: "https://example.com",
		Cadence: "today",
		Extract: "inherit",
		Rev:     3,
		Private: false,
		Health:  feedHealth{NextFetchAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)},
	}
}

// testEntry is a valid Entry wire object with unseen state.
func testEntry() entry {
	return entry{
		ID:             12,
		SubscriptionID: 5,
		FeedID:         2,
		Seq:            100,
		FirstSeenAt:    time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
		Title:          "A headline",
		URL:            "https://example.com/post",
		Author:         "Author",
		Rev:            1,
		State: state{
			Principal: 9,
			EntryID:   12,
			Seen:      register{Value: registerValue{text: "unseen"}},
			Shelf:     register{Value: registerValue{text: "none"}},
			Kept:      register{Value: registerValue{isBool: true}},
			StateRev:  0,
		},
	}
}
