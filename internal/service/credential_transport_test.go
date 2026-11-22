package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

type loopbackResolver struct{}

func (loopbackResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

// namedEgress reaches loopback test servers through non-loopback names, as a
// deployment reaches ollama:11434 or hister:8080 on its Docker network.
func namedEgress(t *testing.T, allowHosts ...string) *outbound.Client {
	t.Helper()
	client, err := outbound.New(outbound.Config{
		Resolver:      loopbackResolver{},
		AllowHosts:    allowHosts,
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Spacing:       time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestLLMKeyFollowsOutboundAllowlist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer sk-test-private" {
			t.Error("provider did not receive its key")
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host := "models.test:" + u.Port()
	f.s.outbound = namedEgress(t, host)
	in := llmSettingsTestInput()
	in.Providers[0].BaseURL = "http://" + host + "/v1"
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, in); err != nil {
		t.Fatalf("save allowlisted provider: %v", err)
	}
	ctx, err := f.s.ModelContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipelineEngine(ctx).Query(ctx, "hello"); err != nil || calls.Load() != 1 {
		t.Fatalf("allowlisted provider: %v, calls %d", err, calls.Load())
	}

	// A restart without the entry keeps the server and its settings usable.
	restarted, err := New(f.db, Deps{SecretKey: []byte(strings.Repeat("s", 32)), Outbound: namedEgress(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = restarted.ModelContext(t.Context())
	if err != nil {
		t.Fatalf("blocked key stopped the model runtime: %v", err)
	}
	if _, err := pipelineEngine(ctx).Query(ctx, "hello"); !errors.Is(err, llm.ErrDisabled) || calls.Load() != 1 {
		t.Fatalf("blocked provider: %v, calls %d", err, calls.Load())
	}
	if status, err := restarted.PipelineStatus(ctx, f.a); err != nil || status.Embed != pipelineStatusDisabled {
		t.Fatalf("status: %+v %v", status, err)
	}
	if got, err := restarted.GetLLMSettings(ctx, f.a); err != nil || !got.Providers[0].HasAPIKey {
		t.Fatalf("settings unreadable: %+v %v", got, err)
	}

	in.Revision, in.OpID, in.Providers[0].APIKey = 1, "retain-key", ""
	if _, err := restarted.PutLLMSettings(ctx, f.a, in); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "EARNEST_OUTBOUND_ALLOW_PRIVATE") || !strings.Contains(err.Error(), `"local"`) {
		t.Fatalf("save must name the provider and the fix: %v", err)
	}
	in.OpID, in.Providers[0].ClearAPIKey = "clear-key", true
	if _, err := restarted.PutLLMSettings(ctx, f.a, in); err != nil {
		t.Fatalf("repair blocked provider: %v", err)
	}
	ctx, err = restarted.ModelContext(t.Context())
	if err != nil || !pipelineEngine(ctx).Enabled(llm.TaskEmbed) {
		t.Fatalf("repaired provider stayed disabled: %v", err)
	}
}

func TestLLMKeyTransportRejectsUnlistedHTTP(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.s.outbound = namedEgress(t, "models.test:11434")
	for _, base := range []string{"http://models.test:11435/v1", "http://other.test:11434/v1", "HTTP://public.example/v1"} {
		t.Run(base, func(t *testing.T) {
			in := llmSettingsTestInput()
			in.Providers[0].BaseURL = base
			if _, err := f.s.PutLLMSettings(t.Context(), f.a, in); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "EARNEST_OUTBOUND_ALLOW_PRIVATE") {
				t.Fatalf("unlisted key transport: %v", err)
			}
		})
	}
}

func TestFeedCredentialsFollowOutboundAllowlist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	prober := &subscriptionsProber{}
	f.s.prober = prober
	f.s.outbound = namedEgress(t, "feeds.test:8080")
	creds := &FeedCredentials{Username: "reader", Password: "private"}
	sub, err := f.s.Subscribe(t.Context(), f.a, "http://feeds.test:8080/rss", SubscribeOptions{OpID: "listed", Credentials: creds})
	if err != nil || !sub.Private {
		t.Fatalf("allowlisted feed: %+v %v", sub, err)
	}
	if _, err := f.s.UpdateSubscription(t.Context(), f.a, sub.ID, UpdateSubscriptionOptions{OpID: "replace", Credentials: creds}); err != nil {
		t.Fatalf("replace credentials: %v", err)
	}
	calls := prober.calls.Load()
	if _, err := f.s.Subscribe(t.Context(), f.a, "http://feeds.test:8081/rss", SubscribeOptions{OpID: "unlisted", Credentials: creds}); !errors.Is(err, ErrInvalid) || prober.calls.Load() != calls {
		t.Fatalf("unlisted feed was probed with credentials: %v", err)
	}
}
