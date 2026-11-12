package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPArticleIntegrations(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	var sends atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/add" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer private-token" {
			t.Errorf("unexpected destination request: %s %s", r.Method, r.URL.Path)
		}
		sends.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer remote.Close()
	u, _ := url.Parse(remote.URL)
	port, _ := strconv.Atoi(u.Port())
	client, err := outbound.New(outbound.Config{Version: "test", AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f.svc, err = service.New(f.db, service.Deps{SecretKey: []byte(strings.Repeat("s", 32)), Outbound: client, MediaPorts: []uint16{uint16(port)}})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = New(f.svc, f.auth, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://reader.example", f.content)

	requireStatus(t, f.do(t, http.MethodGet, "/v1/integrations", nil, requestCredentials{}), http.StatusUnauthorized)
	config := map[string]any{"url": remote.URL, "token": "private-token", "default_label": "Read later", "op_id": "configure"}
	requireStatus(t, f.do(t, http.MethodPut, "/v1/integrations/hister", config, requestCredentials{cookie: f.cookie}), http.StatusForbidden)
	rec := f.do(t, http.MethodPut, "/v1/integrations/hister", config, f.session())
	requireStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "private-token") {
		t.Fatal("write response leaked token")
	}
	rec = f.do(t, http.MethodGet, "/v1/integrations", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	settings := decodeBody[service.ArticleIntegrations](t, rec)
	if !settings.Hister.TokenSet || settings.Hister.DefaultLabel != "Read later" || strings.Contains(rec.Body.String(), "private-token") {
		t.Fatalf("settings: %s", rec.Body.String())
	}
	path := fmt.Sprintf("/v1/entries/%d/send/hister", f.entryID)
	body := map[string]string{"op_id": "send"}
	for range 2 {
		requireStatus(t, f.do(t, http.MethodPost, path, body, f.session()), http.StatusOK)
	}
	if sends.Load() != 1 {
		t.Fatalf("send count: %d", sends.Load())
	}
	agent := requestCredentials{bearer: f.token(t, service.AudienceAPI)}
	requireStatus(t, f.do(t, http.MethodGet, "/v1/integrations", nil, agent), http.StatusForbidden)
	requireStatus(t, f.do(t, http.MethodPut, "/v1/integrations/hister", config, agent), http.StatusForbidden)
	requireStatus(t, f.do(t, http.MethodPost, path, body, agent), http.StatusForbidden)
	requireStatus(t, f.do(t, http.MethodPost, path, body, requestCredentials{cookie: f.cookie}), http.StatusForbidden)
	requireStatus(t, f.do(t, http.MethodPost, path, map[string]string{"op_id": ""}, f.session()), http.StatusBadRequest)
}
