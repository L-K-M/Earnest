package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/L-K-M/Earnest/internal/metrics"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestModelSettingsApplyToHTTPAndWorkers(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	key := bytes.Repeat([]byte("s"), 32)
	svc, err := service.New(db, service.Deps{SecretKey: key})
	if err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuth(db, nil, key)
	owner, err := auth.CreateUser(t.Context(), "models", "password")
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.CreateToken(t.Context(), owner, service.TokenOptions{Kind: service.TokenApp, Name: "native app", Audience: service.AudienceAPI})
	if err != nil {
		t.Fatal(err)
	}
	handler := buildMux("https://app.example", svc, auth, slog.Default(), metrics.New())
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token.Secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	status := request(http.MethodGet, "/v1/pipeline", nil)
	if !bytes.Contains(status.Body.Bytes(), []byte(`"ask":"disabled"`)) {
		t.Fatal(status.Body.String())
	}
	body := service.LLMSettingsInput{OpID: "native-settings", Providers: []service.LLMProviderInput{{Name: "local", Kind: "openai", BaseURL: "https://models.example/v1", MonthlyLimitUSD: "0", TimeoutSeconds: 60, TokenField: "max_tokens"}}, Tasks: []service.LLMTaskSettings{{Task: "ask", Provider: "local", Model: "local-chat"}}, Models: []service.LLMModelSettings{{Name: "local-chat", InputPer1M: "0", OutputPer1M: "0", EmbeddingPer1M: "0"}}}
	request(http.MethodPut, "/v1/settings/llm", body)
	status = request(http.MethodGet, "/v1/pipeline", nil)
	if !bytes.Contains(status.Body.Bytes(), []byte(`"ask":"enabled"`)) {
		t.Fatal("HTTP kept startup-only configuration: " + status.Body.String())
	}
	worker := modelJob(svc, func(ctx context.Context, _ int) (int, error) {
		got, err := svc.PipelineStatus(ctx, owner)
		if err != nil {
			return 0, err
		}
		if got.Ask != "enabled" {
			t.Errorf("worker kept startup-only configuration: %+v", got)
		}
		return 1, nil
	})
	if n, err := worker(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("worker: %d %v", n, err)
	}
}
