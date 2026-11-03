package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/web"
)

func TestContentURLActors(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	app, err := f.auth.CreateToken(t.Context(), f.owner, service.TokenOptions{Name: "app", Kind: service.TokenApp, Audience: service.AudienceAPI})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := f.auth.CreateToken(t.Context(), f.owner, service.TokenOptions{Name: "agent", Kind: service.TokenAgent, Audience: service.AudienceAPI, Scopes: []service.Scope{service.ScopeEntriesRead}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		query         string
		credentials   requestCredentials
		wantMode      web.ImageMode
		wantPrincipal int64
	}{
		{"session click", "?mode=click", f.session(), web.ImageClickToLoad, f.owner.PrincipalID},
		{"app token", "?mode=proxied", requestCredentials{bearer: app.Secret}, web.ImageProxied, f.owner.PrincipalID},
		{"agent token", "?mode=proxied", requestCredentials{bearer: agent.Secret}, web.ImageClickToLoad, agent.Token.PrincipalID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(t, http.MethodGet, fmt.Sprintf("/v1/entries/%d/content-url%s", f.entryID, tc.query), nil, tc.credentials)
			requireStatus(t, rec, http.StatusOK)
			var result contentURLResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.URL == "" || result.ExpiresAt.IsZero() {
				t.Fatalf("result = %+v", result)
			}
			if lifetime := time.Until(result.ExpiresAt); lifetime <= 4*time.Minute || lifetime > 5*time.Minute {
				t.Fatalf("expiry lifetime = %s", lifetime)
			}
			parsed, err := url.Parse(result.URL)
			if err != nil {
				t.Fatal(err)
			}
			ticket, ok := f.content.Signer().Verify(parsed.Query().Get("sig"))
			if !ok {
				t.Fatal("content handler rejected the issued signature")
			}
			if ticket.UserID != f.owner.UserID || ticket.PrincipalID != tc.wantPrincipal || ticket.EntryID != f.entryID {
				t.Fatalf("ticket = %+v", ticket)
			}
			if ticket.Mode != tc.wantMode {
				t.Fatalf("mode = %q, want %q", ticket.Mode, tc.wantMode)
			}
			if !time.Unix(ticket.Expires, 0).UTC().Equal(result.ExpiresAt) {
				t.Fatalf("expires_at = %s, ticket = %d", result.ExpiresAt, ticket.Expires)
			}
			// The URL must be accepted by the content listener itself.
			contentRec := httptest.NewRecorder()
			f.content.ServeHTTP(contentRec, httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil))
			if contentRec.Code != http.StatusOK {
				t.Fatalf("content status = %d: %s", contentRec.Code, contentRec.Body.String())
			}
			if !strings.Contains(contentRec.Body.String(), "Stored content") {
				t.Fatalf("content body = %s", contentRec.Body.String())
			}
		})
	}
}

func TestContentURLRequiresEntriesRead(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	agent, err := f.auth.CreateToken(t.Context(), f.owner, service.TokenOptions{Name: "reader", Kind: service.TokenAgent, Audience: service.AudienceAPI, Scopes: []service.Scope{service.ScopeAnnotationsWrite}})
	if err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, http.MethodGet, fmt.Sprintf("/v1/entries/%d/content-url", f.entryID), nil, requestCredentials{bearer: agent.Secret})
	requireStatus(t, rec, http.StatusForbidden)
	assertProblem(t, rec, "/problems/forbidden")
}

func TestContentURLForeignEntry(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	other, err := f.auth.CreateUser(t.Context(), "other", "password")
	if err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateToken(t.Context(), other, service.TokenOptions{Name: "other-app", Kind: service.TokenApp, Audience: service.AudienceAPI})
	if err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, http.MethodGet, fmt.Sprintf("/v1/entries/%d/content-url", f.entryID), nil, requestCredentials{bearer: token.Secret})
	requireStatus(t, rec, http.StatusNotFound)
	assertProblem(t, rec, "/problems/not-found")
}

func TestContentURLRejectsBadModeAndMissingSigner(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	rec := f.do(t, http.MethodGet, fmt.Sprintf("/v1/entries/%d/content-url?mode=bogus", f.entryID), nil, f.session())
	requireStatus(t, rec, http.StatusUnprocessableEntity)
	assertProblem(t, rec, "/problems/invalid-input")

	app, err := f.auth.CreateToken(t.Context(), f.owner, service.TokenOptions{Name: "app", Kind: service.TokenApp, Audience: service.AudienceAPI})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(f.svc, f.auth, nil, "https://reader.example", nil)
	rec = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/entries/%d/content-url", f.entryID), nil)
	request.Header.Set("Authorization", "Bearer "+app.Secret)
	handler.ServeHTTP(rec, request)
	requireStatus(t, rec, http.StatusServiceUnavailable)
	assertProblem(t, rec, "/problems/unavailable")
}
