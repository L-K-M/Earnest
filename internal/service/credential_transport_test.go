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

func requireTransportRefusal(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "EARNEST_OUTBOUND_ALLOW_PRIVATE") {
		t.Fatalf("%s must be refused with the fix: %v", what, err)
	}
}

func TestIntegrationTokenFollowsOutboundAllowlist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	token := "private-token"
	in := IntegrationSettingsInput{URL: "http://public.example", Token: &token, OpID: "keep-refused"}
	f.s.outbound = namedEgress(t, "hister.test:8080")
	_, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationKarakeep, in)
	requireTransportRefusal(t, "Karakeep token", err)
	in.OpID = "hister-refused"
	_, err = f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in)
	requireTransportRefusal(t, "Hister token", err)
	in.OpID, in.URL = "other-port", "http://hister.test:8081"
	_, err = f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in)
	requireTransportRefusal(t, "token on another port", err)

	in.OpID, in.URL = "listed", "http://hister.test:8080"
	if out, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); err != nil || !out.TokenSet {
		t.Fatalf("allowlisted token: %+v %v", out, err)
	}

	// Settings saved before the entry was removed stay editable and clearable.
	f.s.outbound = namedEgress(t)
	in.OpID, in.Token, in.DefaultLabel = "label", nil, "Reading"
	if out, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); err != nil || !out.TokenSet {
		t.Fatalf("label edit: %+v %v", out, err)
	}
	in.OpID, in.Token = "replace", &token
	_, err = f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in)
	requireTransportRefusal(t, "replacement token", err)
	empty := ""
	in.OpID, in.Token = "clear", &empty
	if out, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); err != nil || out.TokenSet {
		t.Fatalf("clear token: %+v %v", out, err)
	}
	in.OpID, in.URL, in.Token = "tokenless", "http://public.example", nil
	if _, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); err != nil {
		t.Fatalf("tokenless HTTP destination: %v", err)
	}
}

func TestNtfyTokenFollowsOutboundAllowlist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	token := "tk_private"
	in := PushSettingsInput{NtfyServer: "http://public.example", NtfyTopic: "reader", NtfyToken: &token, OpID: "refused"}
	f.s.outbound = namedEgress(t, "ntfy.test:80")
	_, err := f.s.PutPushSettings(ctx, f.a, in)
	requireTransportRefusal(t, "ntfy token", err)

	in.OpID, in.NtfyServer = "listed", "http://ntfy.test"
	if out, err := f.s.PutPushSettings(ctx, f.a, in); err != nil || !out.TokenSet {
		t.Fatalf("allowlisted token: %+v %v", out, err)
	}
	in.OpID, in.NtfyServer, in.NtfyToken = "move", "http://public.example", nil
	_, err = f.s.PutPushSettings(ctx, f.a, in)
	requireTransportRefusal(t, "retained token on a new server", err)

	// Settings saved before the entry was removed stay editable and clearable.
	f.s.outbound = namedEgress(t)
	in.OpID, in.NtfyServer, in.QuietStart, in.QuietEnd = "quiet", "http://ntfy.test", "22:00", "07:00"
	if out, err := f.s.PutPushSettings(ctx, f.a, in); err != nil || !out.TokenSet {
		t.Fatalf("quiet hours edit: %+v %v", out, err)
	}
	in.OpID, in.NtfyToken = "replace", &token
	_, err = f.s.PutPushSettings(ctx, f.a, in)
	requireTransportRefusal(t, "replacement token", err)
	empty := ""
	in.OpID, in.NtfyToken = "clear", &empty
	if out, err := f.s.PutPushSettings(ctx, f.a, in); err != nil || out.TokenSet {
		t.Fatalf("clear token: %+v %v", out, err)
	}
	in.OpID, in.NtfyServer, in.NtfyToken = "tokenless", "http://public.example", nil
	if _, err := f.s.PutPushSettings(ctx, f.a, in); err != nil {
		t.Fatalf("tokenless HTTP server: %v", err)
	}
}

func TestWebhookSignatureFollowsOutboundAllowlist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host := "hooks.test:" + u.Port()
	events := []string{string(WebhookFeedFailing)}
	f.s.outbound = namedEgress(t, host)
	_, err = f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "http://public.example/hook", Events: events}, "refused")
	requireTransportRefusal(t, "new webhook", err)
	hook, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "http://" + host + "/hook?token=private", Events: events}, "listed")
	if err != nil {
		t.Fatalf("allowlisted webhook: %v", err)
	}
	moved := "http://public.example/hook"
	_, err = f.s.UpdateWebhook(ctx, f.a, hook.ID, WebhookPatch{URL: &moved}, "move")
	requireTransportRefusal(t, "new webhook URL", err)

	// A webhook saved before the entry was removed no longer sends.
	if err := f.s.EmitWebhookEvent(ctx, f.a.UserID, WebhookEvent{Type: WebhookFeedFailing, FeedID: 1}); err != nil {
		t.Fatal(err)
	}
	f.s.outbound = namedEgress(t)
	if n, err := f.s.RunWebhookJobs(ctx, 10); err != nil || n != 1 || calls.Load() != 0 {
		t.Fatalf("signature travelled over HTTP: processed %d, calls %d, %v", n, calls.Load(), err)
	}
	if n, active := webhookFailures(t, f, hook.ID); n != 1 || !active {
		t.Fatalf("refusal must be a definite failure: failures %d, active %t", n, active)
	}

	// It stays editable and can be switched off, but not back on.
	same := hook.URL
	if _, err := f.s.UpdateWebhook(ctx, f.a, hook.ID, WebhookPatch{URL: &same, Events: []string{string(WebhookEditionReady)}}, "events"); err != nil {
		t.Fatalf("events edit: %v", err)
	}
	off, on := false, true
	if _, err := f.s.UpdateWebhook(ctx, f.a, hook.ID, WebhookPatch{Active: &off}, "off"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, err = f.s.UpdateWebhook(ctx, f.a, hook.ID, WebhookPatch{Active: &on}, "on")
	requireTransportRefusal(t, "enable", err)
	secure := "https://public.example/hook"
	if out, err := f.s.UpdateWebhook(ctx, f.a, hook.ID, WebhookPatch{URL: &secure, Active: &on}, "secure"); err != nil || !out.Active {
		t.Fatalf("move to HTTPS: %+v %v", out, err)
	}
}

func TestFeedQuerySecretFollowsOutboundAllowlist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	prober := &subscriptionsProber{}
	f.s.prober = prober
	f.s.outbound = namedEgress(t, "feeds.test:8080")
	_, err := f.s.Subscribe(ctx, f.a, "http://public.example/rss?token=private", SubscribeOptions{OpID: "refused"})
	requireTransportRefusal(t, "feed URL secret", err)
	if prober.calls.Load() != 0 || strings.Contains(err.Error(), "private?") {
		t.Fatalf("secret was probed: %v", err)
	}
	for i, raw := range []string{"http://feeds.test:8080/rss?token=private", "http://public.example/?feed=rss2"} {
		if _, err := f.s.Subscribe(ctx, f.a, raw, SubscribeOptions{OpID: "accepted-" + string(rune('a'+i))}); err != nil {
			t.Fatalf("subscribe %q: %v", raw, err)
		}
	}

	results, err := f.s.ImportOPML(ctx, f.a, []OPMLOutline{
		{Title: "Secret", XMLURL: "http://public.example/rss?api_key=hidden-value"},
		{Title: "Plain", XMLURL: "http://public.example/other?feed=rss2"},
	})
	if err != nil || len(results) != 2 || results[1].Subscription == nil {
		t.Fatalf("import: %+v %v", results, err)
	}
	if got := results[0]; got.Subscription != nil || !strings.Contains(got.Error, "EARNEST_OUTBOUND_ALLOW_PRIVATE") || strings.Contains(got.Error, "hidden-value") {
		t.Fatalf("import must name the fix and hide the secret: %+v", got)
	}
}
