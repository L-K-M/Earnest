package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	testBaseURL     = "https://reader.example.test"
	attackerURL     = "https://attacker.example/leak?secret=private"
	protocolVersion = "2026-07-28"
)

type denyProbe struct{ t *testing.T }

func (d denyProbe) Probe(context.Context, string, *service.FeedCredentials) (string, service.FeedMeta, []service.IngestItem, error) {
	d.t.Error("MCP triggered feed discovery/fetching")
	return "", service.FeedMeta{}, nil, errors.New("outbound forbidden")
}

type denyResolver struct{ t *testing.T }

func (d denyResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	d.t.Error("MCP triggered an outbound request")
	return nil, errors.New("outbound forbidden")
}

type fixture struct {
	pipeline   service.Actor
	svc        *service.Service
	auth       *service.Auth
	human      service.Actor
	actor      service.Actor
	token      string
	handler    http.Handler
	entryIDs   []int64
	generation string
}

func newFixture(t *testing.T, template *storetest.Template) *fixture {
	t.Helper()
	ctx := context.Background()
	db := template.New(t)
	key := bytes.Repeat([]byte("k"), 32)
	egress, err := outbound.New(outbound.Config{Resolver: denyResolver{t}})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(db, service.Deps{SecretKey: key, FeedProber: denyProbe{t}, Outbound: egress})
	if err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuth(db, time.Now, key)
	human, err := auth.CreateUser(ctx, "reader", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{svc: svc, auth: auth, human: human}
	pipeline := service.Actor{UserID: human.UserID, Kind: service.PrincipalPipeline, Via: service.CredentialInternal, Scopes: []service.Scope{service.ScopeAnnotationsWrite}}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Summarizer') RETURNING id", human.UserID).Scan(&pipeline.PrincipalID); err != nil {
		t.Fatal(err)
	}
	f.pipeline = pipeline
	for i, cadence := range []service.Cadence{service.CadenceNow, service.CadenceToday, service.CadenceWeek} {
		var feedID int64
		// Include a private feed; agents see all subscriptions, without an allowlist.
		var owner *int64
		if i == 2 {
			owner = &human.UserID
		}
		if err := db.Pool().QueryRow(ctx, "INSERT INTO feeds(url,owner_user_id,title) VALUES($1,$2,$3) RETURNING id", attackerURL+"&feed="+string(cadence), owner, "Feed "+attackerURL).Scan(&feedID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id,cadence) VALUES($1,$2,$3)", human.UserID, feedID, cadence); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(cadence))
		body := `<p>Read <b>this</b> &amp; learn.</p><script>steal()</script><p><a href="` + attackerURL + `">link</a><img src="` + attackerURL + `"></p>`
		_, err := svc.RecordFetch(ctx, service.FetchOutcome{FeedID: feedID, Status: service.FetchOK, Items: []service.IngestItem{{IdentityHash: hex.EncodeToString(hash[:]), ContentHash: hex.EncodeToString(hash[:]), Title: "Article " + string(cadence) + " " + attackerURL, URL: attackerURL, ContentHTML: body, ContentText: "stale text must not be returned", SanitizerVersion: 1, ContentSource: service.ContentFromFeed}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	secret, err := auth.CreateToken(ctx, human, service.TokenOptions{Kind: service.TokenAgent, Name: "Morning", Scopes: service.AgentScopes, Audience: service.AudienceMCP, DailyBudget: 3})
	if err != nil {
		t.Fatal(err)
	}
	f.token = secret.Secret
	f.actor, err = auth.ResolveToken(ctx, f.token, service.AudienceMCP)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := svc.Entries(ctx, f.actor, service.EntryFilter{View: service.ViewAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		f.entryIDs = append(f.entryIDs, e.ID)
	}
	cursor, err := svc.BootstrapCursor(ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	f.generation = cursor.Generation
	f.handler = New(svc, auth, testBaseURL+"/", nil)
	return f
}

func request(t *testing.T, method, name, token string, args any) *http.Request {
	t.Helper()
	params := map[string]any{"_meta": map[string]any{
		"io.modelcontextprotocol/protocolVersion":    protocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "test-agent", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}}
	if name != "" {
		params["name"] = name
		params["arguments"] = args
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, testBaseURL+"/mcp", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("MCP-Protocol-Version", protocolVersion)
	r.Header.Set("Mcp-Method", method)
	if name != "" {
		r.Header.Set("Mcp-Name", name)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func rpc(t *testing.T, handler http.Handler, r *http.Request) json.RawMessage {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Fatal("stateless server issued a session")
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-cache") {
		t.Fatal("missing HTTP cache revalidation policy")
	}
	var envelope struct {
		Result json.RawMessage
		Error  json.RawMessage
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	if len(envelope.Error) != 0 {
		t.Fatalf("RPC error: %s", envelope.Error)
	}
	var resultType struct {
		ResultType string `json:"resultType"`
	}
	if err := json.Unmarshal(envelope.Result, &resultType); err != nil || resultType.ResultType != "complete" {
		t.Fatalf("resultType: %s %v", envelope.Result, err)
	}
	return envelope.Result
}

func call(t *testing.T, f *fixture, name string, args any) *sdk.CallToolResult {
	t.Helper()
	var result sdk.CallToolResult
	raw := rpc(t, f.handler, request(t, "tools/call", name, f.token, args))
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return &result
}

func decodeOutput(t *testing.T, result *sdk.CallToolResult, out any) {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool failed: %+v", result.Content)
	}
	b, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}

func TestStatelessDiscoveryOrderingAndAnnotations(t *testing.T) {
	f := newFixture(t, storetest.NewTemplate(t))
	raw := rpc(t, f.handler, request(t, "server/discover", "", f.token, nil))
	var discovery struct {
		CacheScope        string
		SupportedVersions []string
	}
	if err := json.Unmarshal(raw, &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery.CacheScope != "private" || !slices.Contains(discovery.SupportedVersions, protocolVersion) {
		t.Fatalf("discovery: %s", raw)
	}
	expected := []string{"advance_position", "annotate", "get_briefing", "list_entries", "list_feeds", "notify", "read_entry", "revert", "search", "triage"}
	var first []byte
	for range 3 {
		raw := rpc(t, f.handler, request(t, "tools/list", "", f.token, nil))
		if first == nil {
			first = bytes.Clone(raw)
		} else if !bytes.Equal(first, raw) {
			t.Fatal("tools/list is not deterministic")
		}
		var list sdk.ListToolsResult
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		if list.CacheScope != "private" || list.TTLMs != 0 {
			t.Fatalf("cache: %s", raw)
		}
		var names []string
		for _, tool := range list.Tools {
			names = append(names, tool.Name)
			readOnly := slices.Contains([]string{"get_briefing", "list_entries", "list_feeds", "read_entry", "search"}, tool.Name)
			destructive := slices.Contains([]string{"advance_position", "annotate", "triage", "revert"}, tool.Name)
			a := tool.Annotations
			if a == nil || a.ReadOnlyHint != readOnly || a.DestructiveHint == nil || *a.DestructiveHint != destructive || !a.IdempotentHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
				t.Fatalf("annotations for %s: %+v", tool.Name, a)
			}
			if strings.Contains(tool.Description, attackerURL) {
				t.Fatal("feed data poisoned tool description")
			}
			if !readOnly {
				schemaBytes, _ := json.Marshal(tool.InputSchema)
				var schema struct{ Required []string }
				if err := json.Unmarshal(schemaBytes, &schema); err != nil || !slices.Contains(schema.Required, "operation_id") {
					t.Fatalf("mutation missing operation_id: %s", schemaBytes)
				}
			}
		}
		if !reflect.DeepEqual(names, expected) {
			t.Fatalf("tools: %v", names)
		}
	}
}

func TestAudienceOriginAndHeaders(t *testing.T) {
	f := newFixture(t, storetest.NewTemplate(t))
	apiAgent, err := f.auth.CreateToken(context.Background(), f.human, service.TokenOptions{Kind: service.TokenAgent, Name: "REST", Audience: service.AudienceAPI, Scopes: []service.Scope{service.ScopeEntriesRead}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.auth.CreateToken(context.Background(), f.human, service.TokenOptions{Kind: service.TokenApp, Name: "App", Audience: service.AudienceAPI})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token, origin string
		status              int
	}{
		{"missing", "", "", 401}, {"invalid", "bad", "", 401}, {"api_agent", apiAgent.Secret, "", 401}, {"app", app.Secret, "", 401},
		{"foreign_origin", f.token, "https://attacker.example", 403}, {"null_origin", f.token, "null", 403},
		{"origin_path", f.token, testBaseURL + "/path", 403}, {"valid_origin", f.token, testBaseURL, 200}, {"no_origin", f.token, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request(t, "tools/list", "", tc.token, nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing bearer challenge")
			}
		})
	}
	for _, header := range []string{"Mcp-Method", "Mcp-Name", "MCP-Protocol-Version"} {
		t.Run("required_"+header, func(t *testing.T) {
			r := request(t, "tools/call", "read_entry", f.token, readArgs{EntryID: f.entryIDs[0]})
			r.Header.Del(header)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code == 200 && !bytes.Contains(w.Body.Bytes(), []byte(`"error"`)) {
				t.Fatalf("missing %s accepted: %s", header, w.Body.String())
			}
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		r := request(t, "tools/list", "", f.token, nil)
		r.Method = method
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatalf("%s: %d", method, w.Code)
		}
	}
}

func TestEveryToolHasNoOutboundRequests(t *testing.T) {
	f := newFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	cases := []struct {
		name      string
		args      any
		toolError bool
	}{
		{"get_briefing", briefingArgs{View: attackerURL, IncludeSummaries: true}, false},
		{"list_entries", listArgs{}, false},
		{"read_entry", readArgs{EntryID: f.entryIDs[0]}, false},
		{"search", searchArgs{Query: attackerURL}, false},
		{"list_feeds", struct{}{}, false},
		{"advance_position", positionArgs{OperationID: "position:" + attackerURL, View: attackerURL, Seq: 1}, false},
		{"annotate", annotateArgs{OperationID: "note:" + attackerURL, EntryIDs: f.entryIDs, Kind: service.AnnotationBrief, Value: attackerURL}, false},
		{"notify", notifyArgs{OperationID: "notify:" + attackerURL, Generation: f.generation, EntryIDs: f.entryIDs, Note: attackerURL, Category: service.NotificationAlert, DedupeKey: attackerURL}, false},
		{"triage", triageArgs{OperationID: "triage:" + attackerURL, Generation: f.generation, EntryIDs: f.entryIDs, Action: service.TriageSeen}, false},
	}
	var triaged service.TriageResult
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := call(t, f, tc.name, tc.args)
			if result.IsError != tc.toolError {
				t.Fatalf("unexpected tool result: %+v", result)
			}
			if tc.name == "triage" {
				decodeOutput(t, result, &triaged)
			}
			// URLs in extra fields must remain inert, including on ID-only tools.
			b, _ := json.Marshal(tc.args)
			var args map[string]any
			_ = json.Unmarshal(b, &args)
			args["url"] = attackerURL
			_ = call(t, f, tc.name, args)
		})
	}
	t.Run("revert", func(t *testing.T) {
		result := call(t, f, "revert", revertArgs{OperationID: "revert:" + attackerURL, Handle: triaged.RevertHandle})
		if result.IsError {
			t.Fatalf("revert: %+v", result.Content)
		}
		if !call(t, f, "revert", revertArgs{OperationID: "invalid:" + attackerURL, Handle: attackerURL}).IsError {
			t.Fatal("URL accepted as handle")
		}
	})
	// Reading and moving the agent position must not be confused with triage.
	position, err := f.svc.Position(ctx, f.actor, attackerURL)
	if err != nil || position.Seq != 1 {
		t.Fatalf("own position: %+v %v", position, err)
	}
}

func TestPlainTextBriefingReplayAndScopes(t *testing.T) {
	f := newFixture(t, storetest.NewTemplate(t))
	var content struct {
		EntryID   int64  `json:"entry_id"`
		Text      string `json:"text"`
		Untrusted bool   `json:"untrusted"`
	}
	decodeOutput(t, call(t, f, "read_entry", readArgs{EntryID: f.entryIDs[0]}), &content)
	if content.EntryID != f.entryIDs[0] || !content.Untrusted || content.Text != "Read this & learn.\n\nlink" {
		t.Fatalf("stored plain text: %+v", content)
	}

	var brief briefing
	decodeOutput(t, call(t, f, "get_briefing", briefingArgs{}), &brief)
	if brief.Generation != f.generation || len(brief.Groups) != 3 || brief.ThroughSeq != 3 || brief.HasMore || !brief.Untrusted {
		t.Fatalf("briefing: %+v", brief)
	}
	for i, group := range brief.Groups {
		if len(group.Entries) != 1 || group.Entries[0].ID != f.entryIDs[i] || len(group.Entries[0].Summaries) != 0 {
			t.Fatalf("group: %+v", group)
		}
	}
	var feeds struct{ Feeds []service.Subscription }
	decodeOutput(t, call(t, f, "list_feeds", struct{}{}), &feeds)
	if len(feeds.Feeds) != 3 || !feeds.Feeds[2].Private {
		t.Fatalf("feeds: %+v", feeds)
	}
	summary, err := f.svc.PutAnnotation(context.Background(), f.pipeline, service.PutAnnotationRequest{OpID: "stored-summary", EntryIDs: []int64{f.entryIDs[0]}, Kind: service.AnnotationSummary, Value: "An existing summary"})
	if err != nil {
		t.Fatal(err)
	}
	var summarized briefing
	decodeOutput(t, call(t, f, "get_briefing", briefingArgs{IncludeSummaries: true}), &summarized)
	if got := summarized.Groups[0].Entries[0].Summaries; len(got) != 1 || got[0].ID != summary.ID || !got[0].Untrusted {
		t.Fatalf("stored summary: %+v", got)
	}
	summary2, err := f.svc.PutAnnotation(context.Background(), f.pipeline, service.PutAnnotationRequest{OpID: "stored-summary-2", EntryIDs: []int64{f.entryIDs[1]}, Kind: service.AnnotationSummary, Value: "Second summary"})
	if err != nil {
		t.Fatal(err)
	}
	var multi briefing
	decodeOutput(t, call(t, f, "get_briefing", briefingArgs{IncludeSummaries: true}), &multi)
	if got := multi.Groups[1].Entries[0].Summaries; len(got) != 1 || got[0].ID != summary2.ID {
		t.Fatalf("second stored summary: %+v", got)
	}

	var first briefing
	decodeOutput(t, call(t, f, "get_briefing", briefingArgs{Limit: 1}), &first)
	if first.ThroughSeq != 1 || !first.HasMore {
		t.Fatalf("first page: %+v", first)
	}
	decodeOutput(t, call(t, f, "advance_position", positionArgs{OperationID: "advance", Seq: 1}), &map[string]any{})
	decodeOutput(t, call(t, f, "get_briefing", briefingArgs{}), &brief)
	if len(brief.Groups[0].Entries) != 0 || brief.ThroughSeq != 3 {
		t.Fatalf("resume: %+v", brief)
	}

	humanPosition, err := f.svc.Position(context.Background(), f.human, briefingView)
	if err != nil || humanPosition.Seq != 0 {
		t.Fatalf("agent advanced human position: %+v %v", humanPosition, err)
	}
	humanEntries, err := f.svc.Entries(context.Background(), f.human, service.EntryFilter{View: service.ViewAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range humanEntries {
		if !entry.State.Unread() {
			t.Fatal("read tool changed human state")
		}
	}

	args := notifyArgs{OperationID: "one-alert", Generation: f.generation, EntryIDs: f.entryIDs, Note: "A real alert", Category: service.NotificationAlert}
	var sent, replay service.Notification
	decodeOutput(t, call(t, f, "notify", args), &sent)
	decodeOutput(t, call(t, f, "notify", args), &replay)
	if sent.ID != replay.ID || !replay.Replay || sent.Budget.UsedToday != 1 || replay.Budget != sent.Budget {
		t.Fatalf("replay: %+v %+v", sent, replay)
	}
	notifications, err := f.svc.ListNotifications(context.Background(), f.human, 0, 100)
	if err != nil || len(notifications) != 1 {
		t.Fatalf("notifications: %+v %v", notifications, err)
	}

	limited, err := f.auth.CreateToken(context.Background(), f.human, service.TokenOptions{Kind: service.TokenAgent, Name: "Read only", Audience: service.AudienceMCP, Scopes: []service.Scope{service.ScopeEntriesRead}})
	if err != nil {
		t.Fatal(err)
	}
	f.token = limited.Secret
	result := call(t, f, "notify", args)
	if !result.IsError {
		t.Fatal("agent without notify:send sent a notification")
	}
}

func TestBoundsAndLLMS(t *testing.T) {
	for _, tc := range []struct {
		input, want int
		invalid     bool
	}{{0, 50, false}, {1, 1, false}, {200, 200, false}, {201, 0, true}, {-1, 0, true}} {
		got, err := pageSize(tc.input)
		if got != tc.want || errors.Is(err, service.ErrInvalid) != tc.invalid {
			t.Fatalf("pageSize(%d) = %d, %v", tc.input, got, err)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		w := httptest.NewRecorder()
		LLMS(w, httptest.NewRequest(method, "/llms.txt", nil))
		if method == http.MethodPost {
			if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal(w)
			}
			continue
		}
		if w.Code != 200 || w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
			t.Fatal(w)
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
		if method == http.MethodGet && (!strings.Contains(w.Body.String(), "only tool") || !strings.Contains(w.Body.String(), "untrusted")) {
			t.Fatal("missing safety guide")
		}
	}
}

func TestEquivalentBrowserOrigins(t *testing.T) {
	for _, tc := range []struct {
		base, origin string
		status       int
	}{
		{"https://reader.example.test:443/base", "https://reader.example.test", 401},
		{"http://reader.example.test:80", "http://reader.example.test", 401},
		{"https://READER.example.test", "https://reader.example.test", 401},
		{"https://reader.example.test", "https://reader.example.test:443", 401},
		{"https://reader.example.test:8443", "https://reader.example.test", 403},
		{testBaseURL, testBaseURL + "/path", 403},
		{testBaseURL, testBaseURL + "?query=1", 403},
		{testBaseURL, "https://reader.example.test@attacker.example", 403},
	} {
		t.Run(tc.base+"/"+tc.origin, func(t *testing.T) {
			handler := New(nil, nil, tc.base, nil)
			r := request(t, "tools/list", "", "", nil)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("Origin %q for %q: got %d, want %d", tc.origin, tc.base, w.Code, tc.status)
			}
		})
	}
}

func TestToolErrorPrivacy(t *testing.T) {
	internal := errors.New("query failed: password=private internal_database_detail")
	if got := safeToolError(internal); got.Error() != "internal error" {
		t.Fatalf("internal detail exposed: %v", got)
	}
	for _, domain := range []error{service.ErrInvalid, service.ErrUnauthorized, service.ErrForbidden, service.ErrNotFound, service.ErrConflict, service.ErrGone, service.ErrRateLimited, service.ErrUnavailable} {
		if got := safeToolError(domain); !errors.Is(got, domain) {
			t.Fatalf("lost domain error: %v", got)
		}
	}
}

// Search returns stored entries, each marked untrusted.
func TestSearchFindsStoredEntries(t *testing.T) {
	f := newFixture(t, storetest.NewTemplate(t))
	var out struct {
		Results []searchHit `json:"results"`
	}
	decodeOutput(t, call(t, f, "search", searchArgs{Query: "Article"}), &out)
	if len(out.Results) == 0 {
		t.Fatal("search found no stored entries")
	}
	for _, hit := range out.Results {
		if !hit.Untrusted || hit.EntryID == 0 {
			t.Fatalf("hit = %+v, want an untrusted entry match", hit)
		}
	}
}
