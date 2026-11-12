package llm

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProviderRedirectDoesNotLeakCredentials(t *testing.T) {
	for _, kind := range []ProviderKind{KindAnthropic, KindGemini} {
		t.Run(string(kind), func(t *testing.T) {
			var reached atomic.Bool
			server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirected" {
					reached.Store(true)
					t.Errorf("redirect target received credentials: anthropic=%q gemini=%q", r.Header.Get("x-api-key"), r.Header.Get("x-goog-api-key"))
					io.WriteString(w, `{}`)
					return
				}
				http.Redirect(w, r, "http://localhost:"+strings.Split(r.Host, ":")[1]+"/redirected", http.StatusTemporaryRedirect)
			})
			provider := newTestProvider(t, kind, server.URL)
			_, err := provider.Complete(t.Context(), CompleteRequest{Model: "test"})
			if err == nil || reached.Load() {
				t.Fatalf("redirect followed=%v, error=%v", reached.Load(), err)
			}
		})
	}
}

func TestRejectedCompletionRecordsUsage(t *testing.T) {
	responses := map[ProviderKind]string{
		KindOpenAI:    `{"choices":[{"message":{"content":"invalid"}}],"usage":{"prompt_tokens":100,"completion_tokens":50}}`,
		KindAnthropic: `{"content":[{"type":"text","text":"invalid"}],"usage":{"input_tokens":100,"output_tokens":50}}`,
		KindGemini:    `{"candidates":[{"content":{"parts":[{"text":"invalid"}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50}}`,
	}
	for kind, response := range responses {
		t.Run(string(kind), func(t *testing.T) {
			server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, response) })
			client, spend := newTestClient(t, newTestProvider(t, kind, server.URL), nil, map[string]ModelInfo{"test-model": {InputPer1M: 1000000, OutputPer1M: 1000000}})
			for range 3 {
				result, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{Schema: labelSchema(t)})
				if result != nil || !errors.Is(err, ErrInvalidOutput) {
					t.Fatalf("result=%v error=%v", result, err)
				}
			}
			if spend.spent != 450 {
				t.Fatalf("recorded spend=%d, want 450", spend.spent)
			}
		})
	}
}

func TestRejectedEmbeddingRecordsUsage(t *testing.T) {
	provider := &fakeProvider{name: "test", embed: func(req EmbedRequest) (*EmbedResult, error) {
		return &EmbedResult{Vectors: [][]float32{{float32(math.NaN())}}, Usage: Usage{Input: 150}}, nil
	}}
	client, spend := newTestClient(t, provider, nil, map[string]ModelInfo{"test-model": {EmbeddingPer1M: 1000000}})
	result, _, err := client.Embed(context.Background(), TaskBlurb, EmbedRequest{Texts: []string{"example"}})
	if result != nil || !errors.Is(err, ErrDimensions) {
		t.Fatalf("result=%v error=%v", result, err)
	}
	if spend.spent != 150 {
		t.Fatalf("recorded spend=%d, want 150", spend.spent)
	}
}

func TestDefaultEmbeddingUsesSmallerNativeWidth(t *testing.T) {
	called := false
	provider := &fakeProvider{name: "local", embed: func(req EmbedRequest) (*EmbedResult, error) {
		called = true
		if req.Dimensions != 768 {
			t.Errorf("dimensions=%d, want 768", req.Dimensions)
		}
		return &EmbedResult{Vectors: [][]float32{make([]float32, 768)}}, nil
	}}
	client, _ := newTestClient(t, provider, nil, map[string]ModelInfo{"test-model": {Dimensions: 768}})
	vectors, _, err := client.Embed(t.Context(), TaskBlurb, EmbedRequest{Texts: []string{"text"}})
	if err != nil {
		t.Fatal(err)
	}
	if !called || len(vectors) != 1 || len(vectors[0].Values) != StorageDimensions || vectors[0].SourceDimensions != 768 {
		t.Fatalf("called=%v vectors=%v", called, vectors)
	}
}

func TestCappedGeminiEmbeddingsCannotBeUnmetered(t *testing.T) {
	var called atomic.Bool
	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		io.WriteString(w, `{"embedding":{"values":[0.1]}}`)
	})
	client, _ := newTestClient(t, newTestProvider(t, KindGemini, server.URL), map[string]USD{"test": 100}, map[string]ModelInfo{"test-model": {EmbeddingPer1M: 1000000}})
	// newTestProvider uses the configured name "test".
	_, _, err := client.Embed(t.Context(), TaskBlurb, EmbedRequest{Texts: []string{"text"}})
	if err == nil || called.Load() {
		t.Fatalf("unmetered capped request sent=%v error=%v", called.Load(), err)
	}
}

func TestRequestCannotExceedRemainingBudget(t *testing.T) {
	called := false
	provider := &fakeProvider{name: "paid", complete: func(req CompleteRequest) (*Completion, error) {
		called = true
		return &Completion{Text: "billed", Usage: Usage{Output: 150}}, nil
	}}
	client, spend := newTestClient(t, provider, map[string]USD{"paid": 100}, map[string]ModelInfo{"test-model": {OutputPer1M: 1000000}})
	result, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 150})
	if !errors.Is(err, ErrBudgetExhausted) || called || result != nil {
		t.Fatalf("provider called=%v, spend=%d, result=%v, error=%v", called, spend.spent, result, err)
	}
}

func TestMalformedEmbeddingRecordsUsage(t *testing.T) {
	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[],"usage":{"prompt_tokens":150}}`)
	})
	client, spend := newTestClient(t, newTestProvider(t, KindOpenAI, server.URL), nil, map[string]ModelInfo{"test-model": {EmbeddingPer1M: 1000000}})
	vectors, _, err := client.Embed(t.Context(), TaskBlurb, EmbedRequest{Texts: []string{"text"}})
	if err == nil || vectors != nil {
		t.Fatalf("vectors=%v error=%v", vectors, err)
	}
	if spend.spent != 150 {
		t.Fatalf("recorded spend=%d, want 150", spend.spent)
	}
}

func TestRouterRejectsUnmeteredCappedGeminiEmbeddings(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{"gemini": {Kind: KindGemini, BaseURL: "https://provider.test", MonthlyLimitUSD: 100}},
		Tasks:     map[Task]TaskConfig{TaskEmbed: {Provider: "gemini", Model: "embedding"}},
		Models:    map[string]ModelInfo{"embedding": {EmbeddingPer1M: 1000000}},
	}
	if _, err := NewRouter(cfg, testOutbound(t)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("capped unmetered configuration accepted: %v", err)
	}
}

func TestConcurrentRequestsReserveRemainingBudget(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	provider := &fakeProvider{name: "paid", complete: func(req CompleteRequest) (*Completion, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return &Completion{Text: "billed", Usage: Usage{Output: 60}}, nil
	}}
	client, spend := newTestClient(t, provider, map[string]USD{"paid": 100}, map[string]ModelInfo{"test-model": {OutputPer1M: 1000000}})
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60})
		firstDone <- err
	}()
	<-firstStarted
	_, secondErr := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60})
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(secondErr, ErrBudgetExhausted) || calls.Load() != 1 || spend.spent != 60 {
		t.Fatalf("calls=%d spend=%d second error=%v", calls.Load(), spend.spent, secondErr)
	}
}
