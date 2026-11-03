package llm

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAnthropicComplete(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"content":[{"type":"text","text":"{\"label\":\"press_release\"}"}],"usage":{"input_tokens":9,"output_tokens":4}}`)
	})
	provider := newTestProvider(t, KindAnthropic, server.URL)
	completion, err := provider.Complete(t.Context(), CompleteRequest{
		Model:       "claude-test",
		System:      "Classify the article.",
		Messages:    []Message{{Role: RoleUser, Content: "Article text"}},
		Schema:      labelSchema(t),
		MaxTokens:   128,
		Temperature: 0.1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := cap.one(t)
	if request.Path != "/v1/messages" {
		t.Errorf("path = %s, want /v1/messages", request.Path)
	}
	if got := request.Header.Get("x-api-key"); got != "secret-key" {
		t.Errorf("x-api-key = %q", got)
	}
	if got := request.Header.Get("anthropic-version"); got != anthropicVersion {
		t.Errorf("anthropic-version = %q", got)
	}
	body := decodeBody(t, request)
	if body["model"] != "claude-test" {
		t.Errorf("model = %v", body["model"])
	}
	if body["max_tokens"] != float64(128) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
	// Anthropic has no native schema mode, so the schema travels in the
	// instruction and Validate enforces it.
	system, _ := body["system"].(string)
	if !strings.Contains(system, `"label"`) || !strings.Contains(system, "JSON") {
		t.Errorf("system = %q", system)
	}
	if completion.Text != `{"label":"press_release"}` {
		t.Errorf("text = %q", completion.Text)
	}
	if completion.Usage != (Usage{Input: 9, Output: 4}) {
		t.Errorf("usage = %v", completion.Usage)
	}
}

func TestAnthropicDefaultMaxTokens(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"usage":{}}`)
	})
	provider := newTestProvider(t, KindAnthropic, server.URL)
	if _, err := provider.Complete(t.Context(), CompleteRequest{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, cap.one(t))
	if body["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", body["max_tokens"], defaultMaxTokens)
	}
}

func TestAnthropicEmbedUnsupported(t *testing.T) {
	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the server")
	})
	provider := newTestProvider(t, KindAnthropic, server.URL)
	_, err := provider.Embed(t.Context(), EmbedRequest{Model: "m", Texts: []string{"a"}})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}
