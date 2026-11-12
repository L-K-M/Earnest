package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

func TestDiscoverModelsDialects(t *testing.T) {
	for _, tc := range []struct {
		kind                                      ProviderKind
		path, header, auth, first, second, cursor string
	}{
		{KindOpenAI, "/models", "Authorization", "Bearer secret-key", `{"data":[{"id":"z"}],"has_more":true,"last_id":"z"}`, `{"data":[{"id":"a"},{"id":"z"}]}`, "after"},
		{KindAnthropic, "/v1/models", "x-api-key", "secret-key", `{"data":[{"id":"z","display_name":"Zed"}],"has_more":true,"last_id":"z"}`, `{"data":[{"id":"a"}]}`, "after_id"},
		{KindGemini, "/models", "x-goog-api-key", "secret-key", `{"models":[{"name":"models/z","displayName":"Zed","supportedGenerationMethods":["generateContent"]}],"nextPageToken":"z"}`, `{"models":[{"name":"models/a","supportedGenerationMethods":["embedContent"]},{"name":"models/image","supportedGenerationMethods":["predict"]}]}`, "pageToken"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != tc.path || r.Header.Get(tc.header) != tc.auth {
					t.Errorf("wrong catalogue request: %s %s", r.Method, r.URL.Path)
				}
				if tc.kind == KindAnthropic && r.Header.Get("anthropic-version") != anthropicVersion {
					t.Error("missing version")
				}
				if requests == 1 {
					io.WriteString(w, tc.first)
				} else {
					if r.URL.Query().Get(tc.cursor) != "z" {
						t.Error("missing pagination cursor")
					}
					io.WriteString(w, tc.second)
				}
			}))
			defer server.Close()
			models, err := DiscoverModels(t.Context(), "test", ProviderConfig{Kind: tc.kind, BaseURL: server.URL, APIKey: "secret-key"}, testOutbound(t))
			if err != nil || len(models) != 2 || requests != 2 {
				t.Fatalf("catalogue: %+v %v, requests %d", models, err, requests)
			}
			if models[0].ID != "a" || models[1].ID != "z" {
				t.Fatalf("not sorted/deduplicated: %+v", models)
			}
			if tc.kind == KindOpenAI && (models[0].Chat != nil || models[0].InputPer1M != nil) {
				t.Fatal("guessed metadata")
			}
			if tc.kind == KindGemini && (models[0].Embedding == nil || !*models[0].Embedding || models[0].Chat == nil || *models[0].Chat) {
				t.Fatal("incorrect Gemini capabilities")
			}
		})
	}
}

func TestVeniceCatalogue(t *testing.T) {
	raw := []byte(`{"data":[
 {"id":"chat","type":"text","model_spec":{"name":"Chat","pricing":{"input":{"usd":0.15},"output":{"usd":0.6}}}},
 {"id":"embed","type":"embedding","model_spec":{"pricing":{"input":{"usd":0.01}}}},
 {"id":"unknown-price","type":"text"},
 {"id":"image","type":"image"}, {"id":"audio","type":"tts"}
 ]}`)
	models, _, err := decodeCatalogue(raw, KindOpenAI, true)
	if err != nil || len(models) != 3 {
		t.Fatalf("%+v %v", models, err)
	}
	if models[0].InputPer1M == nil || *models[0].InputPer1M != "0.150000" || *models[0].OutputPer1M != "0.600000" {
		t.Fatalf("wrong price: %+v", models[0])
	}
	if !*models[1].Embedding || *models[1].Chat || *models[1].EmbeddingPer1M != "0.010000" {
		t.Fatalf("wrong embedding: %+v", models[1])
	}
	if models[2].InputPer1M != nil || models[1].Dimensions != nil {
		t.Fatal("missing metadata became a default")
	}
	for _, price := range []string{"", "-1", "0.1234567", "100000000", "1e2"} {
		if cataloguePrice(json.Number(price)) != nil {
			t.Errorf("accepted unsupported price %q", price)
		}
	}
	if cataloguePrice(json.Number("0")) == nil {
		t.Fatal("explicit free price lost")
	}
}

func TestDiscoverModelsLargeCatalogue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models := make([]map[string]string, 200)
		for i := range models {
			models[i] = map[string]string{"id": fmt.Sprintf("model-%03d", i)}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": models})
	}))
	defer server.Close()
	models, err := DiscoverModels(t.Context(), "test", ProviderConfig{Kind: KindOpenAI, BaseURL: server.URL}, testOutbound(t))
	if err != nil || len(models) != 200 {
		t.Fatalf("catalogue incorrectly limited to configured-model count: %d %v", len(models), err)
	}
}

func TestDiscoverModelsFailuresAreRedacted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"authentication", 401, "secret-key"}, {"permissions", 403, "secret-key"}, {"missing", 404, "secret-key"},
		{"rate limit", 429, "secret-key"}, {"upstream", 500, "secret-key"}, {"redirect", 302, "secret-key"},
		{"invalid JSON", 200, `{"secret-key`}, {"wrong shape", 200, `{"error":"secret-key"}`},
		{"null", 200, `{"data":null}`}, {"invalid ID", 200, `{"data":[{"id":"bad model"}]}`},
		{"missing cursor", 200, `{"data":[],"has_more":true}`},
		{"repeated cursor", 200, `{"data":[],"has_more":true,"last_id":"again"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://127.0.0.1/secret-key")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := DiscoverModels(t.Context(), "test", ProviderConfig{Kind: KindOpenAI, BaseURL: server.URL, APIKey: "secret-key"}, testOutbound(t))
			if err == nil || strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("unsafe or absent error: %v", err)
			}
		})
	}
}

func TestDiscoverModelsEmptyAndBounds(t *testing.T) {
	models, _, err := decodeCatalogue([]byte(`{"data":[]}`), KindOpenAI, false)
	if err != nil || models == nil || len(models) != 0 {
		t.Fatalf("empty catalogue: %+v %v", models, err)
	}
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		fmt.Fprintf(w, `{"data":[],"has_more":true,"last_id":"%d"}`, pages)
	}))
	defer server.Close()
	_, err = DiscoverModels(t.Context(), "test", ProviderConfig{Kind: KindOpenAI, BaseURL: server.URL}, testOutbound(t))
	if err == nil || pages != maxCataloguePages {
		t.Fatalf("unbounded pagination: %d %v", pages, err)
	}
}

func TestDiscoverModelsOutboundAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client, _ := outbound.New(outbound.Config{})
	_, err := DiscoverModels(t.Context(), "test", ProviderConfig{Kind: KindOpenAI, BaseURL: server.URL}, client)
	if err == nil {
		t.Fatal("private host was not blocked")
	}
	_, err = DiscoverModels(t.Context(), "test", ProviderConfig{Kind: KindOpenAI, BaseURL: server.URL, Timeout: 20 * time.Millisecond}, testOutbound(t))
	if err == nil {
		t.Fatal("timeout was not enforced")
	}
}

// Map the real catalogue hostname to our local fixture without making a live
// provider request. This verifies that Venice's query is not sent to every
// OpenAI-compatible server.
type catalogueResolver struct{}

func (catalogueResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestVeniceDiscoveryRequestsAllTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "all" {
			t.Error("Venice request omitted embedding models")
		}
		io.WriteString(w, `{"data":[{"id":"chat","type":"text"},{"id":"embed","type":"embedding"},{"id":"image","type":"image"}]}`)
	}))
	defer server.Close()
	client, err := outbound.New(outbound.Config{Resolver: catalogueResolver{}, AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	models, err := DiscoverModels(t.Context(), "venice", ProviderConfig{Kind: KindOpenAI, BaseURL: strings.Replace(server.URL, "127.0.0.1", "api.venice.ai", 1) + "/api/v1"}, client)
	if err != nil || len(models) != 2 {
		t.Fatalf("Venice discovery: %+v %v", models, err)
	}
}
