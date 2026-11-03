package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/imageproxy"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/L-K-M/Earnest/internal/web"
)

type fixtureProber struct{}

func (fixtureProber) Probe(ctx context.Context, raw string, credentials *service.FeedCredentials) (string, service.FeedMeta, []service.IngestItem, error) {
	hash := sha256.Sum256([]byte("entry"))
	return raw, service.FeedMeta{Title: "Test feed", Language: "english"}, []service.IngestItem{{IdentityHash: hex.EncodeToString(hash[:]), ContentHash: hex.EncodeToString(hash[:]), Title: "Test entry", URL: "https://example.org/article", ContentHTML: "<p>Stored content</p>", ContentText: "Stored content", SanitizerVersion: 1, ContentSource: service.ContentFromFeed}}, nil
}

const fixtureContentOrigin = "https://content.example"

type apiFixture struct {
	db      *store.Store
	svc     *service.Service
	auth    *service.Auth
	owner   service.Actor
	content *web.Content
	handler http.Handler
	entryID int64
	subID   int64
	cookie  *http.Cookie
	csrf    string
}
type requestCredentials struct {
	cookie *http.Cookie
	csrf   string
	bearer string
}

func newAPIFixture(t *testing.T, template *storetest.Template) *apiFixture {
	t.Helper()
	db := template.New(t)
	secret := bytes.Repeat([]byte{5}, 32)
	auth := service.NewAuth(db, nil, secret)
	owner, err := auth.CreateUser(t.Context(), "reader", "password")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(db, service.Deps{SecretKey: secret, FeedProber: fixtureProber{}})
	if err != nil {
		t.Fatal(err)
	}
	egress, err := outbound.New(outbound.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := imageproxy.New(secret, egress, fixtureContentOrigin)
	if err != nil {
		t.Fatal(err)
	}
	content, err := web.NewContent(svc, auth, proxy, fixtureContentOrigin, secret)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := svc.Subscribe(t.Context(), owner, "https://example.org/feed", service.SubscribeOptions{OpID: "setup"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := svc.Entries(t.Context(), owner, service.EntryFilter{View: service.ViewAll})
	if err != nil || len(entries) != 1 {
		t.Fatalf("fixture entries=%v: %v", entries, err)
	}
	f := &apiFixture{db: db, svc: svc, auth: auth, owner: owner, content: content, handler: New(svc, auth, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://reader.example", content), entryID: entries[0].ID, subID: sub.ID}
	rec := f.do(t, http.MethodPost, "/v1/session/login", loginBody{Username: "reader", Password: "password"}, requestCredentials{})
	requireStatus(t, rec, http.StatusOK)
	var login loginResult
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	f.csrf = login.CSRFToken
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	f.cookie = cookies[0]
	return f
}
func (f *apiFixture) session() requestCredentials {
	return requestCredentials{cookie: f.cookie, csrf: f.csrf}
}
func (f *apiFixture) do(t *testing.T, method, path string, body any, credentials requestCredentials) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if credentials.cookie != nil {
		request.AddCookie(credentials.cookie)
	}
	if credentials.csrf != "" {
		request.Header.Set(csrfHeader, credentials.csrf)
	}
	if credentials.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+credentials.bearer)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, request)
	return rec
}
func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status=%d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}
func (f *apiFixture) token(t *testing.T, audience service.Audience) string {
	t.Helper()
	token, err := f.auth.CreateToken(t.Context(), f.owner, service.TokenOptions{Name: "agent", Kind: service.TokenAgent, Audience: audience, Scopes: []service.Scope{service.ScopeEntriesRead, service.ScopeStateWriteHuman, service.ScopePositionOwn}})
	if err != nil {
		t.Fatal(err)
	}
	return token.Secret
}
func (f *apiFixture) write(t *testing.T, op string, register syncmodel.RegisterName, value syncmodel.Value) syncmodel.Write {
	t.Helper()
	cursor, err := f.svc.BootstrapCursor(t.Context(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return syncmodel.Write{OpID: op, Principal: syncmodel.ID(f.owner.PrincipalID), EntryID: syncmodel.ID(f.entryID), Register: register, Value: value, Base: syncmodel.Revision{Generation: cursor.Generation}, OccurredAt: time.Now().UTC(), Kind: syncmodel.Explicit}
}

func TestHTTPReader(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("session and state", func(t *testing.T) {
		f := newAPIFixture(t, template)
		if !f.cookie.HttpOnly || !f.cookie.Secure || f.cookie.SameSite != http.SameSiteLaxMode || f.cookie.Path != "/" {
			t.Fatalf("unsafe cookie: %+v", f.cookie)
		}
		rec := f.do(t, http.MethodGet, "/v1/me", nil, requestCredentials{})
		requireStatus(t, rec, http.StatusUnauthorized)
		assertProblem(t, rec, "/problems/unauthorized")
		requireStatus(t, f.do(t, http.MethodGet, "/v1/me", nil, f.session()), http.StatusOK)
		body := []syncmodel.Write{f.write(t, "keep", syncmodel.Kept, syncmodel.True)}
		rec = f.do(t, http.MethodPost, "/v1/state:batch", body, requestCredentials{cookie: f.cookie})
		requireStatus(t, rec, http.StatusForbidden)
		assertProblem(t, rec, "/problems/forbidden")
		rec = f.do(t, http.MethodPost, "/v1/state:batch", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		var results []syncmodel.Result
		if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].Status != syncmodel.Applied || results[0].State.Kept.Value != syncmodel.True {
			t.Fatalf("results=%+v", results)
		}
		rec = f.do(t, http.MethodGet, fmt.Sprintf("/v1/entries/%d", f.entryID), nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		var entry struct {
			State syncmodel.State `json:"state"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		if entry.State.Kept.Value != syncmodel.True {
			t.Fatalf("state round trip=%+v", entry.State)
		}
		requireStatus(t, f.do(t, http.MethodPost, "/v1/session/logout", nil, f.session()), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodGet, "/v1/me", nil, f.session()), http.StatusUnauthorized)
	})
	t.Run("agent boundaries and audience", func(t *testing.T) {
		f := newAPIFixture(t, template)
		credentials := requestCredentials{bearer: f.token(t, service.AudienceAPI)}
		for _, path := range []string{"/v1/entries?view=all", fmt.Sprintf("/v1/entries?ids=%d", f.entryID), fmt.Sprintf("/v1/entries/%d/content", f.entryID), "/v1/subscriptions", "/v1/bootstrap", "/v1/bootstrap/entries"} {
			requireStatus(t, f.do(t, http.MethodGet, path, nil, credentials), http.StatusOK)
		}
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, "/v1/tokens", nil},
			{http.MethodPost, "/v1/tokens", tokenBody{Kind: service.TokenAgent, Name: "forbidden", Audience: service.AudienceAPI}},
			{http.MethodDelete, "/v1/tokens/1", nil},
			{http.MethodPost, "/v1/devices/start", deviceStart{ClientName: "agent"}},
			{http.MethodPost, "/v1/devices/poll", devicePoll{DeviceCode: "unknown"}},
			{http.MethodPost, "/v1/devices/approve", deviceApprove{UserCode: "ABCD-EFGH"}},
			{http.MethodPost, fmt.Sprintf("/v1/subscriptions/%d/refresh", f.subID), opBody{OpID: "denied"}},
			{http.MethodPost, "/v1/subscriptions", subscriptionBody{URL: "https://example.org/new", OpID: "denied"}},
			{http.MethodPatch, fmt.Sprintf("/v1/subscriptions/%d", f.subID), subscriptionPatch{OpID: "denied"}},
			{http.MethodDelete, fmt.Sprintf("/v1/subscriptions/%d?op_id=denied", f.subID), nil},
		} {
			t.Run(tc.method+tc.path, func(t *testing.T) {
				rec := f.do(t, tc.method, tc.path, tc.body, credentials)
				requireStatus(t, rec, http.StatusForbidden)
				assertProblem(t, rec, "/problems/forbidden")
			})
		}
		requireStatus(t, f.do(t, http.MethodGet, "/v1/entries", nil, requestCredentials{bearer: f.token(t, service.AudienceMCP)}), http.StatusUnauthorized)
		// Even a supplied deliberate source cannot change an agent's provenance.
		write := stateWrite{Write: f.write(t, "agent-seen", syncmodel.Seen, syncmodel.SeenValue), Source: "deliberate"}
		rec := f.do(t, http.MethodPost, "/v1/state:batch", []stateWrite{write}, credentials)
		requireStatus(t, rec, http.StatusOK)
		var results []syncmodel.Result
		if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].State.Seen.Source != syncmodel.SourceRuleOrAgent {
			t.Fatalf("agent source=%+v", results)
		}
	})
	t.Run("expired change cursor", func(t *testing.T) {
		f := newAPIFixture(t, template)
		cursor, err := f.svc.BootstrapCursor(t.Context(), f.owner)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.CreateFolder(t.Context(), f.owner, "changed", "new-folder"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.RetainChanges(t.Context(), time.Now().Add(100*24*time.Hour), 90*24*time.Hour); err != nil {
			t.Fatal(err)
		}
		rec := f.do(t, http.MethodGet, fmt.Sprintf("/v1/changes?generation=%s&rev=%d", cursor.Generation, cursor.Rev), nil, f.session())
		requireStatus(t, rec, http.StatusGone)
		assertProblem(t, rec, "/problems/gone")
		rec = f.do(t, http.MethodGet, "/v1/changes?cursor=previous-generation:0", nil, f.session())
		requireStatus(t, rec, http.StatusGone)
	})
	t.Run("refresh scheduling", func(t *testing.T) {
		f := newAPIFixture(t, template)
		sub, err := f.svc.Subscription(t.Context(), f.owner, f.subID)
		if err != nil {
			t.Fatal(err)
		}
		future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
		lease := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
		if _, err := f.db.Pool().Exec(t.Context(), "UPDATE feeds SET next_fetch_at=$2,lease_until=$3 WHERE id=$1", sub.FeedID, future, lease); err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/v1/subscriptions/%d/refresh", f.subID)
		requireStatus(t, f.do(t, http.MethodPost, path, opBody{OpID: "refresh"}, f.session()), http.StatusOK)
		var next, afterLease time.Time
		if err := f.db.Pool().QueryRow(t.Context(), "SELECT next_fetch_at,lease_until FROM feeds WHERE id=$1", sub.FeedID).Scan(&next, &afterLease); err != nil {
			t.Fatal(err)
		}
		if next.After(time.Now()) || !afterLease.Equal(lease) {
			t.Fatalf("refresh next=%s lease=%s", next, afterLease)
		}
		if _, err := f.db.Pool().Exec(t.Context(), "UPDATE feeds SET next_fetch_at=$2 WHERE id=$1", sub.FeedID, future); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, f.do(t, http.MethodPost, path, opBody{OpID: "refresh"}, f.session()), http.StatusOK)
		if err := f.db.Pool().QueryRow(t.Context(), "SELECT next_fetch_at FROM feeds WHERE id=$1", sub.FeedID).Scan(&next); err != nil {
			t.Fatal(err)
		}
		if !next.Equal(future) {
			t.Fatal("replayed refresh scheduled another fetch")
		}
		other, err := f.auth.CreateUser(t.Context(), "other", "password")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.RefreshSubscription(t.Context(), other, f.subID, "foreign"); !errors.Is(err, service.ErrNotFound) {
			t.Fatalf("foreign refresh=%v", err)
		}
		for _, tc := range []struct {
			id int64
			op string
		}{{-1, "negative"}, {f.subID, ""}} {
			if err := f.svc.RefreshSubscription(t.Context(), f.owner, tc.id, tc.op); !errors.Is(err, service.ErrInvalid) {
				t.Fatalf("invalid refresh=%v", err)
			}
		}
	})
	t.Run("device flow", func(t *testing.T) {
		f := newAPIFixture(t, template)
		start := func() deviceResult {
			rec := f.do(t, http.MethodPost, "/v1/devices/start", deviceStart{ClientName: "native"}, requestCredentials{})
			requireStatus(t, rec, http.StatusOK)
			var out deviceResult
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		pending := start()
		rec := f.do(t, http.MethodPost, "/v1/devices/poll", devicePoll{DeviceCode: pending.DeviceCode}, requestCredentials{})
		requireStatus(t, rec, http.StatusBadRequest)
		var p problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.ErrorCode != "authorization_pending" {
			t.Fatalf("device error=%+v", p)
		}
		rec = f.do(t, http.MethodPost, "/v1/devices/poll", devicePoll{DeviceCode: pending.DeviceCode}, requestCredentials{})
		requireStatus(t, rec, http.StatusBadRequest)
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.ErrorCode != "slow_down" {
			t.Fatalf("device error=%+v", p)
		}
		approved := start()
		requireStatus(t, f.do(t, http.MethodPost, "/v1/devices/approve", deviceApprove{UserCode: approved.UserCode}, f.session()), http.StatusOK)
		rec = f.do(t, http.MethodPost, "/v1/devices/poll", devicePoll{DeviceCode: approved.DeviceCode}, requestCredentials{})
		requireStatus(t, rec, http.StatusOK)
		var token deviceToken
		if err := json.Unmarshal(rec.Body.Bytes(), &token); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, f.do(t, http.MethodGet, "/v1/me", nil, requestCredentials{bearer: token.AccessToken}), http.StatusOK)
	})
	t.Run("OPML raw and multipart", func(t *testing.T) {
		f := newAPIFixture(t, template)
		data := []byte(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Folder"><outline text="New" xmlUrl="https://example.org/new"/></outline></body></opml>`)
		upload := func(body []byte, contentType string) {
			request := httptest.NewRequest(http.MethodPost, "/v1/opml:import", bytes.NewReader(body))
			request.Header.Set("Content-Type", contentType)
			request.Header.Set(csrfHeader, f.csrf)
			request.AddCookie(f.cookie)
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, request)
			requireStatus(t, rec, http.StatusOK)
		}
		upload(data, "application/xml")
		var buf bytes.Buffer
		form := multipart.NewWriter(&buf)
		if err := form.WriteField("description", "upload"); err != nil {
			t.Fatal(err)
		}
		file, err := form.CreateFormFile("file", "subscriptions.opml")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		upload(buf.Bytes(), form.FormDataContentType())
		rec := f.do(t, http.MethodGet, "/v1/opml", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		doc, err := feed.ParseOPML(rec.Body.Bytes())
		if err != nil || len(doc.Outlines) == 0 {
			t.Fatalf("export=%s: %v", rec.Body.String(), err)
		}
	})
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, kind string) {
	t.Helper()
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("media type=%s", rec.Header().Get("Content-Type"))
	}
	var p problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Type != kind || p.Status != rec.Code || p.Title == "" {
		t.Fatalf("problem=%+v", p)
	}
}

func TestSSECommittedState(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.token(t, service.AudienceAPI))
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE status=%d, media type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	readEvent := func() syncmodel.Revision {
		t.Helper()
		var cursor syncmodel.Revision
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\n" {
				return cursor
			}
			if strings.HasPrefix(line, "data: ") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cursor); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	initial := readEvent()
	write := f.write(t, "sse-state", syncmodel.Shelf, syncmodel.Later)
	if _, err := f.svc.Batch(ctx, f.owner, []syncmodel.Write{write}); err != nil {
		t.Fatal(err)
	}
	hint := readEvent()
	if hint.Generation != initial.Generation || hint.Rev <= initial.Rev {
		t.Fatalf("initial=%+v hint=%+v", initial, hint)
	}
	// Last-Event-ID is validated before the stream's success headers are written.
	rec := httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	bad.Header.Set("Last-Event-ID", "old-generation:0")
	bad.AddCookie(f.cookie)
	f.handler.ServeHTTP(rec, bad)
	requireStatus(t, rec, http.StatusGone)
}

func TestOpenAPIIsCurrent(t *testing.T) {
	generated, err := OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, committed) {
		t.Fatal("api/openapi.json is stale; run go run ./cmd/earnest openapi > api/openapi.json")
	}
	rec := httptest.NewRecorder()
	New(nil, nil, nil, "", nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if !bytes.Equal(rec.Body.Bytes(), generated) {
		t.Fatal("served OpenAPI differs from command output")
	}
}

func TestLoginReplacesExistingSessionWithoutCSRF(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	rec := f.do(t, http.MethodPost, "/v1/session/login", loginBody{Username: "reader", Password: "password"}, requestCredentials{cookie: f.cookie})
	requireStatus(t, rec, http.StatusOK)
	request := httptest.NewRequest(http.MethodPost, "/v1/session/login", strings.NewReader(`{"username":"reader","password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	request.AddCookie(f.cookie)
	denied := httptest.NewRecorder()
	f.handler.ServeHTTP(denied, request)
	requireStatus(t, denied, http.StatusForbidden)
}

func TestPublicDeviceAuthWithSessionCookie(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, mode := range []string{"stale", "without_csrf"} {
		t.Run(mode, func(t *testing.T) {
			f := newAPIFixture(t, template)
			credentials := requestCredentials{cookie: f.cookie}
			if mode == "stale" {
				credentials.cookie = &http.Cookie{Name: sessionCookie, Value: "expired"}
			}
			rec := f.do(t, http.MethodPost, "/v1/devices/start", deviceStart{ClientName: "Reader"}, credentials)
			requireStatus(t, rec, http.StatusOK)
			code := decodeBody[deviceResult](t, rec).DeviceCode
			rec = f.do(t, http.MethodPost, "/v1/devices/poll", devicePoll{DeviceCode: code}, credentials)
			requireStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), "authorization_pending") {
				t.Fatalf("poll did not reach device authorization: %s", rec.Body)
			}
			// Protected writes still require a valid session and CSRF token.
			rec = f.do(t, http.MethodPost, "/v1/folders", nameBody{Name: "Folder", OpID: "folder"}, credentials)
			if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
				t.Fatalf("protected write accepted: %d", rec.Code)
			}
		})
	}
}
