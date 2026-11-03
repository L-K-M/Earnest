package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// testOutbound returns an outbound client that may reach a loopback test
// server. Non-standard ports are allowed by the provider constructors, which
// derive them from the base URL.
func testOutbound(t *testing.T) *outbound.Client {
	t.Helper()
	client, err := outbound.New(outbound.Config{
		Version:       "test",
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Spacing:       time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// newTestProvider builds a provider for a test server, with a key set so the
// auth header can be asserted.
func newTestProvider(t *testing.T, kind ProviderKind, baseURL string) Provider {
	t.Helper()
	provider, err := newProvider("test", ProviderConfig{
		Kind:       kind,
		BaseURL:    baseURL,
		APIKey:     "secret-key",
		TokenField: TokenFieldCompletion,
	}, testOutbound(t))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

// capturedRequest is what a test server received.
type capturedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// capture collects requests that reached a test server.
type capture struct {
	requests chan capturedRequest
}

// newCapture starts a server that records every request and then calls
// handler to respond.
func newCapture(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{requests: make(chan capturedRequest, 16)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.requests <- capturedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, cap
}

// one waits for the next recorded request.
func (c *capture) one(t *testing.T) capturedRequest {
	t.Helper()
	select {
	case request := <-c.requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("no request reached the test server")
		return capturedRequest{}
	}
}

// decodeBody unmarshals a recorded request body.
func decodeBody(t *testing.T, request capturedRequest) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
}

// TestNoTools proves the threat-model invariant that Earnest's own models
// cannot be given tools: the request type has no field that could carry a tool
// definition, and no provider puts one on the wire.
func TestNoTools(t *testing.T) {
	requestType := reflect.TypeOf(CompleteRequest{})
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if strings.Contains(strings.ToLower(field.Name), "tool") {
			t.Errorf("CompleteRequest.%s could carry a tool definition", field.Name)
		}
	}

	// The provider interface itself must not accept tools, so no
	// implementation can grow a tool parameter without breaking the
	// pipeline's compile-time contract.
	providerType := reflect.TypeOf((*Provider)(nil)).Elem()
	for i := 0; i < providerType.NumMethod(); i++ {
		method := providerType.Method(i)
		for j := 0; j < method.Type.NumIn(); j++ {
			if strings.Contains(strings.ToLower(method.Type.In(j).String()), "tool") {
				t.Errorf("Provider.%s accepts %s", method.Name, method.Type.In(j))
			}
		}
	}

	schema, err := NewSchema("label", json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ProviderKind{KindOpenAI, KindAnthropic, KindGemini} {
		t.Run(string(kind), func(t *testing.T) {
			server, cap := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch kind {
				case KindOpenAI:
					io.WriteString(w, `{"choices":[{"message":{"content":"{\"label\":\"x\"}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
				case KindAnthropic:
					io.WriteString(w, `{"content":[{"type":"text","text":"{\"label\":\"x\"}"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
				case KindGemini:
					io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"label\":\"x\"}"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`)
				}
			})
			provider := newTestProvider(t, kind, server.URL)
			if _, err := provider.Complete(t.Context(), CompleteRequest{
				Model:    "test-model",
				System:   "Classify the article.",
				Schema:   schema,
				Messages: []Message{{Role: RoleUser, Content: "article"}},
			}); err != nil {
				t.Fatal(err)
			}
			request := cap.one(t)
			body := decodeBody(t, request)
			if _, ok := body["tools"]; ok {
				t.Errorf("request body carries tools: %v", body["tools"])
			}
			if strings.Contains(string(request.Body), `"tools"`) {
				t.Error("request body mentions tools")
			}
		})
	}
}

// TestProviderDeniesPrivateAddress shows that a local model server is only
// reachable when the outbound client's private allowlist names it, which is
// the operator's explicit opt-in.
func TestProviderDeniesPrivateAddress(t *testing.T) {
	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach a private address")
	})
	client, err := outbound.New(outbound.Config{Version: "test", Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider("local", ProviderConfig{Kind: KindOpenAI, BaseURL: server.URL}, client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(t.Context(), CompleteRequest{Model: "m"})
	var providerErr *Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if providerErr.Retryable {
		t.Error("a denied address is not retryable")
	}
}

// TestNoSecretsOrPromptsInLogs captures the debug stream and checks that an
// API key and a prompt never appear, at any level.
func TestNoSecretsOrPromptsInLogs(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	})
	provider := newTestProvider(t, KindOpenAI, server.URL)
	if _, err := provider.Complete(t.Context(), CompleteRequest{
		Model:    "m",
		System:   "system secret prompt",
		Messages: []Message{{Role: RoleUser, Content: "article secret prompt"}},
	}); err != nil {
		t.Fatal(err)
	}
	logs := buf.String()
	if strings.Contains(logs, "secret-key") {
		t.Error("API key reached the log")
	}
	if strings.Contains(logs, "secret prompt") {
		t.Error("prompt reached the log")
	}
}

// TestProviderErrorMapping checks that HTTP failures become *Error with the
// status and a retry decision a caller can act on.
func TestProviderErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		retryable bool
	}{
		{"unauthorized", http.StatusUnauthorized, false},
		{"forbidden", http.StatusForbidden, false},
		{"not_found", http.StatusNotFound, false},
		{"rate_limited", http.StatusTooManyRequests, true},
		{"server_error", http.StatusInternalServerError, true},
		{"bad_gateway", http.StatusBadGateway, true},
	}
	for _, kind := range []ProviderKind{KindOpenAI, KindAnthropic, KindGemini} {
		for _, test := range tests {
			t.Run(string(kind)+"_"+test.name, func(t *testing.T) {
				server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(test.status)
					io.WriteString(w, `{"error":{"message":"nope"}}`)
				})
				provider := newTestProvider(t, kind, server.URL)
				_, err := provider.Complete(t.Context(), CompleteRequest{Model: "test-model"})
				var providerErr *Error
				if !errors.As(err, &providerErr) {
					t.Fatalf("error %v is not *Error", err)
				}
				if providerErr.StatusCode != test.status {
					t.Errorf("status = %d, want %d", providerErr.StatusCode, test.status)
				}
				if providerErr.Retryable != test.retryable {
					t.Errorf("retryable = %v, want %v", providerErr.Retryable, test.retryable)
				}
			})
		}
	}
}
