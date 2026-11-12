package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPLLMSettings(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	requireStatus(t, f.do(t, http.MethodGet, "/v1/settings/llm", nil, requestCredentials{}), http.StatusUnauthorized)
	agent := requestCredentials{bearer: f.token(t, service.AudienceAPI)}
	requireStatus(t, f.do(t, http.MethodGet, "/v1/settings/llm", nil, agent), http.StatusForbidden)
	rec := f.do(t, http.MethodGet, "/v1/settings/llm", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("configuration reads must not be cached")
	}
	defaults := decodeBody[map[string]any](t, rec)
	if defaults["revision"] != float64(0) || len(defaults["providers"].([]any)) != 0 || len(defaults["tasks"].([]any)) != 7 {
		t.Fatalf("unexpected defaults: %s", rec.Body.String())
	}
	body := map[string]any{
		"revision": 0, "op_id": "configure-models",
		"providers": []any{map[string]any{"name": "local", "kind": "openai", "base_url": "https://models.example/v1", "api_key": "sk-private-key", "clear_api_key": false, "monthly_limit_usd": "10", "timeout_seconds": 60, "token_field": "max_tokens"}},
		"tasks":     []any{map[string]any{"task": "blurb", "provider": "local", "model": "local-chat"}},
		"models":    []any{map[string]any{"name": "local-chat", "input_per_1m": "0", "output_per_1m": "0", "embedding_per_1m": "0", "dimensions": 0, "truncatable": false}},
	}
	requireStatus(t, f.do(t, http.MethodPut, "/v1/settings/llm", body, agent), http.StatusForbidden)
	requireStatus(t, f.do(t, http.MethodPut, "/v1/settings/llm", body, requestCredentials{cookie: f.cookie}), http.StatusForbidden)
	rec = f.do(t, http.MethodPut, "/v1/settings/llm", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "sk-private-key") || strings.Contains(rec.Body.String(), `"api_key"`) {
		t.Fatal("PUT disclosed a provider key")
	}
	saved := decodeBody[map[string]any](t, rec)
	if saved["revision"] != float64(1) || saved["providers"].([]any)[0].(map[string]any)["has_api_key"] != true {
		t.Fatalf("save: %s", rec.Body.String())
	}
	// Retries are idempotent even though the supplied revision is now stale.
	requireStatus(t, f.do(t, http.MethodPut, "/v1/settings/llm", body, f.session()), http.StatusOK)
	body["op_id"] = "stale-models"
	requireStatus(t, f.do(t, http.MethodPut, "/v1/settings/llm", body, f.session()), http.StatusConflict)
	rec = f.do(t, http.MethodGet, "/v1/settings/llm", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "sk-private-key") {
		t.Fatal("GET disclosed a provider key")
	}
	// Clients may omit both optional key controls to preserve the stored key.
	provider := body["providers"].([]any)[0].(map[string]any)
	delete(provider, "api_key")
	delete(provider, "clear_api_key")
	body["revision"], body["op_id"] = 1, "keep-key"
	rec = f.do(t, http.MethodPut, "/v1/settings/llm", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	saved = decodeBody[map[string]any](t, rec)
	if saved["providers"].([]any)[0].(map[string]any)["has_api_key"] != true {
		t.Fatal("omitting key controls removed the saved key")
	}
	provider["clear_api_key"] = true
	body["revision"], body["op_id"] = 2, "clear-key"
	rec = f.do(t, http.MethodPut, "/v1/settings/llm", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	saved = decodeBody[map[string]any](t, rec)
	if saved["providers"].([]any)[0].(map[string]any)["has_api_key"] != false {
		t.Fatal("explicit clear did not remove the key")
	}
}

func TestLLMSettingsProductionMount(t *testing.T) {
	_, document := build(nil, nil, nil, "", nil)
	if document.OpenAPI().Paths["/v1/settings/llm"] == nil {
		t.Fatal("the router does not mount /v1/settings/llm")
	}
}
