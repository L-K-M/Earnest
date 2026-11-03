package llm

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGeminiFinishReason(t *testing.T) {
	for _, reason := range []string{"", "STOP", "MAX_TOKENS", "SAFETY", "RECITATION", "OTHER"} {
		t.Run("reason="+reason, func(t *testing.T) {
			server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{"candidates":[{"finishReason":`+quoteJSON(reason)+`,"content":{"parts":[{"text":"{\"label\":\"news\"}"}]}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":6}}`)
			})
			provider := newTestProvider(t, KindGemini, server.URL)
			result, err := provider.Complete(t.Context(), CompleteRequest{Model: "test-model", Schema: labelSchema(t)})
			if reason == "" || reason == "STOP" {
				if err != nil || result == nil || result.Text != `{"label":"news"}` {
					t.Fatalf("result=%+v error=%v", result, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidOutput) || !strings.Contains(err.Error(), reason) {
				t.Fatalf("finishReason=%s accepted: error=%v", reason, err)
			}
			if result == nil || result.Text != "" || result.Usage != (Usage{Input: 12, Output: 6}) {
				t.Fatalf("rejected result must retain usage only: %+v", result)
			}

			client, spend := newTestClient(t, provider, nil, map[string]ModelInfo{"test-model": {InputPer1M: 1000000, OutputPer1M: 1000000}})
			completion, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{Schema: labelSchema(t)})
			if !errors.Is(err, ErrInvalidOutput) || completion != nil || spend.spent != 18 {
				t.Fatalf("completion=%+v spend=%d error=%v", completion, spend.spent, err)
			}
		})
	}
}

func TestGeminiComplete(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"label\":\"press_release\"}"}]}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":6,"totalTokenCount":18}}`)
	})
	provider := newTestProvider(t, KindGemini, server.URL)
	completion, err := provider.Complete(t.Context(), CompleteRequest{
		Model:       "gemini-test",
		System:      "Classify the article.",
		Messages:    []Message{{Role: RoleUser, Content: "Article text"}},
		Schema:      labelSchema(t),
		MaxTokens:   256,
		Temperature: 0.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := cap.one(t)
	if request.Path != "/models/gemini-test:generateContent" {
		t.Errorf("path = %s", request.Path)
	}
	if got := request.Header.Get("x-goog-api-key"); got != "secret-key" {
		t.Errorf("x-goog-api-key = %q", got)
	}
	body := decodeBody(t, request)
	config, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig = %v", body["generationConfig"])
	}
	if config["maxOutputTokens"] != float64(256) {
		t.Errorf("maxOutputTokens = %v", config["maxOutputTokens"])
	}
	if config["temperature"] != float64(0.3) {
		t.Errorf("temperature = %v", config["temperature"])
	}
	if config["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v", config["responseMimeType"])
	}
	instruction, ok := body["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("systemInstruction = %v", body["systemInstruction"])
	}
	parts := instruction["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "Classify the article." {
		t.Errorf("systemInstruction = %v", instruction)
	}
	contents, ok := body["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %v", body["contents"])
	}
	first := contents[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("role = %v", first["role"])
	}
	if completion.Text != `{"label":"press_release"}` {
		t.Errorf("text = %q", completion.Text)
	}
	if completion.Usage != (Usage{Input: 12, Output: 6}) {
		t.Errorf("usage = %v", completion.Usage)
	}
}

func TestGeminiEmbed(t *testing.T) {
	server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"embedding":{"values":[0.1,0.2,0.3]}}`)
	})
	provider := newTestProvider(t, KindGemini, server.URL)
	result, err := provider.Embed(t.Context(), EmbedRequest{
		Model:      "embed-model",
		Texts:      []string{"first", "second"},
		Dimensions: StorageDimensions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Vectors) != 2 {
		t.Fatalf("vectors = %d", len(result.Vectors))
	}
	for i, want := range []string{"first", "second"} {
		request := cap.one(t)
		if request.Path != "/models/embed-model:embedContent" {
			t.Errorf("path = %s", request.Path)
		}
		body := decodeBody(t, request)
		if body["outputDimensionality"] != float64(StorageDimensions) {
			t.Errorf("outputDimensionality = %v", body["outputDimensionality"])
		}
		content := body["content"].(map[string]any)
		parts := content["parts"].([]any)
		if parts[0].(map[string]any)["text"] != want {
			t.Errorf("text %d = %v", i, parts[0])
		}
		if len(result.Vectors[i]) != 3 || result.Vectors[i][2] != 0.3 {
			t.Errorf("vector %d = %v", i, result.Vectors[i])
		}
	}
}
