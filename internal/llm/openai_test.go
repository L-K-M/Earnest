package llm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// labelSchema is a strict structured-output schema used across provider tests.
func labelSchema(t *testing.T) *Schema {
	t.Helper()
	schema, err := NewSchema("label", json.RawMessage(`{
		"type": "object",
		"properties": {"label": {"type": "string"}},
		"required": ["label"],
		"additionalProperties": false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestOpenAIComplete(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"label\":\"press_release\"}"}}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
	})
	provider := newTestProvider(t, KindOpenAI, server.URL)
	completion, err := provider.Complete(t.Context(), CompleteRequest{
		Model:       "test-model",
		System:      "Classify the article.",
		Messages:    []Message{{Role: RoleUser, Content: "Article text"}},
		Schema:      labelSchema(t),
		MaxTokens:   100,
		Temperature: 0.2,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := cap.one(t)
	if request.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", request.Method)
	}
	if request.Path != "/chat/completions" {
		t.Errorf("path = %s, want /chat/completions", request.Path)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("Authorization = %q", got)
	}
	body := decodeBody(t, request)
	if body["model"] != "test-model" {
		t.Errorf("model = %v", body["model"])
	}
	if body["max_completion_tokens"] != float64(100) {
		t.Errorf("max_completion_tokens = %v", body["max_completion_tokens"])
	}
	if body["temperature"] != float64(0.2) {
		t.Errorf("temperature = %v", body["temperature"])
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "Classify the article." {
		t.Errorf("system message = %v", first)
	}
	format, ok := body["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format = %v", body["response_format"])
	}
	if format["type"] != "json_schema" {
		t.Errorf("response_format type = %v", format["type"])
	}
	spec := format["json_schema"].(map[string]any)
	if spec["name"] != "label" || spec["strict"] != true {
		t.Errorf("json_schema = %v", spec)
	}
	if completion.Text != `{"label":"press_release"}` {
		t.Errorf("text = %q", completion.Text)
	}
	if completion.Usage != (Usage{Input: 11, Output: 7}) {
		t.Errorf("usage = %v", completion.Usage)
	}
}

func TestOpenAICompleteLegacyTokenField(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	provider, err := newProvider("test", ProviderConfig{
		Kind:       KindOpenAI,
		BaseURL:    server.URL,
		APIKey:     "secret-key",
		TokenField: TokenFieldLegacy,
	}, testOutbound(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Complete(t.Context(), CompleteRequest{Model: "m", MaxTokens: 32}); err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, cap.one(t))
	if body["max_tokens"] != float64(32) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Error("max_completion_tokens should be absent")
	}
}

func TestOpenAIInvalidOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{"not_json", "the label is press_release"},
		{"wrong_type", `{"label":7}`},
		{"missing_required", `{}`},
		{"extra_property", `{"label":"x","extra":1}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{"choices":[{"message":{"content":`+quoteJSON(test.output)+`}}],"usage":{}}`)
			})
			provider := newTestProvider(t, KindOpenAI, server.URL)
			_, err := provider.Complete(t.Context(), CompleteRequest{Model: "m", Schema: labelSchema(t)})
			if !errors.Is(err, ErrInvalidOutput) {
				t.Fatalf("error = %v, want ErrInvalidOutput", err)
			}
		})
	}
}

func TestOpenAICompleteLongOutput(t *testing.T) {
	long := strings.Repeat("x", 2048)
	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":`+quoteJSON(long)+`}}],"usage":{}}`)
	})
	provider := newTestProvider(t, KindOpenAI, server.URL)
	completion, err := provider.Complete(t.Context(), CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Text != long {
		t.Errorf("text length = %d, want %d", len(completion.Text), len(long))
	}
}

func TestOpenAIEmbed(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"index":1,"embedding":[0.2,0.4]},{"index":0,"embedding":[0.1,0.3]}],"usage":{"prompt_tokens":5,"total_tokens":5}}`)
	})
	provider := newTestProvider(t, KindOpenAI, server.URL)
	result, err := provider.Embed(t.Context(), EmbedRequest{
		Model:      "embed-model",
		Texts:      []string{"first", "second"},
		Dimensions: StorageDimensions,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := cap.one(t)
	if request.Path != "/embeddings" {
		t.Errorf("path = %s, want /embeddings", request.Path)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("Authorization = %q", got)
	}
	body := decodeBody(t, request)
	if body["model"] != "embed-model" {
		t.Errorf("model = %v", body["model"])
	}
	if body["dimensions"] != float64(StorageDimensions) {
		t.Errorf("dimensions = %v", body["dimensions"])
	}
	inputs, ok := body["input"].([]any)
	if !ok || len(inputs) != 2 || inputs[0] != "first" || inputs[1] != "second" {
		t.Errorf("input = %v", body["input"])
	}
	// Responses may arrive out of order; the provider restores input order.
	if len(result.Vectors) != 2 {
		t.Fatalf("vectors = %d", len(result.Vectors))
	}
	if result.Vectors[0][0] != 0.1 || result.Vectors[1][0] != 0.2 {
		t.Errorf("vectors out of order: %v", result.Vectors)
	}
	if result.Usage.Input != 5 {
		t.Errorf("usage = %v", result.Usage)
	}
}

func TestOpenAIEmbedCountMismatch(t *testing.T) {
	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1]}],"usage":{}}`)
	})
	provider := newTestProvider(t, KindOpenAI, server.URL)
	_, err := provider.Embed(t.Context(), EmbedRequest{Model: "m", Texts: []string{"a", "b"}})
	if err == nil {
		t.Fatal("expected an error for a short response")
	}
}

// quoteJSON renders s as a JSON string literal for embedding in a response.
func quoteJSON(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}
