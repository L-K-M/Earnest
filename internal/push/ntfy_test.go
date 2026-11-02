package push

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func TestNtfySend(t *testing.T) {
	srv, captured := newCapture(t, http.StatusCreated, nil)
	transport, err := NewNtfy(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := transport.Send(context.Background(), Device{
		Kind:     KindNtfy,
		Endpoint: srv.URL + "/morning",
		Token:    "tk_secret",
	}, Message{
		Title:          "Morning Agent: 1 alert",
		Body:           "One new alert",
		NotificationID: "n_551",
		Category:       CategoryAlert,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "" {
		t.Fatalf("receipt = %q", receipt.ProviderID)
	}

	req := captured.one(t)
	captured.requireCount(t, 1)
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s", req.Method)
	}
	for _, forbidden := range []string{"Icon", "Attach", "Click", "Actions", "Email", "Call", "Markdown"} {
		if got := req.Header.Get(forbidden); got != "" {
			t.Errorf("forbidden header %s = %q", forbidden, got)
		}
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tk_secret" {
		t.Fatalf("authorization = %q", got)
	}
	if got := req.Header.Get("Priority"); got != ntfyPriorityHigh {
		t.Fatalf("priority = %q", got)
	}
	if got := req.Header.Get("Title"); got != "Morning Agent: 1 alert" {
		t.Fatalf("title = %q", got)
	}
	if got := string(req.Body); got != "One new alert" {
		t.Fatalf("body = %q", got)
	}
	if strings.Contains(strings.ToLower(string(req.Body)), "://") {
		t.Fatal("body carries a URL")
	}
}

func TestNtfyBasicAuth(t *testing.T) {
	srv, captured := newCapture(t, http.StatusCreated, nil)
	transport, err := NewNtfy(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(context.Background(), Device{
		Kind:     KindNtfy,
		Endpoint: srv.URL + "/topic",
		Username: "user",
		Password: "pass",
	}, Message{Title: "t", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	req := captured.one(t)
	captured.requireCount(t, 1)
	raw := req.Header.Get("Authorization")
	if !strings.HasPrefix(raw, "Basic ") {
		t.Fatalf("authorization = %q", raw)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, "Basic "))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(decoded); got != "user:pass" {
		t.Fatalf("basic credentials = %q", got)
	}
}

func TestNtfyTextRules(t *testing.T) {
	tests := []struct {
		name       string
		showText   bool
		selfHosted bool
		agentText  string
		wantBody   string
	}{
		{"opted out", false, true, "words", "the body"},
		{"third party server", true, false, "words", "the body"},
		{"self-hosted", true, true, "words", "the body\n\nwords"},
		{"url in text is dropped", true, true, "see https://example.net/x", "the body"},
		{"no agent text", true, true, "", "the body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, captured := newCapture(t, http.StatusCreated, nil)
			transport, err := NewNtfy(testConfig(t, srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), Device{
				Kind:       KindNtfy,
				Endpoint:   srv.URL + "/topic",
				ShowText:   tt.showText,
				SelfHosted: tt.selfHosted,
			}, Message{Title: "t", Body: "the body", AgentText: tt.agentText})
			if err != nil {
				t.Fatal(err)
			}
			req := captured.one(t)
			captured.requireCount(t, 1)
			if got := string(req.Body); got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// The ntfy request body must never carry a URL, whatever the caller put in
// the server-generated message.
func TestNtfyRejectsURLInServerBody(t *testing.T) {
	srv, captured := newCapture(t, http.StatusCreated, nil)
	transport, err := NewNtfy(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(context.Background(), Device{Kind: KindNtfy, Endpoint: srv.URL + "/topic"}, Message{
		Title: "t",
		Body:  "read https://example.net/story",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if OutcomeOf(err) != OutcomeNotDelivered {
		t.Fatalf("outcome = %v", OutcomeOf(err))
	}
	select {
	case <-captured.requests:
		t.Fatal("request with a URL was sent anyway")
	default:
	}
}

func TestNtfyPriority(t *testing.T) {
	tests := []struct {
		category Category
		want     string
	}{
		{CategoryAlert, ntfyPriorityHigh},
		{Category("digest"), ntfyPriorityDefault},
		{Category(""), ntfyPriorityDefault},
	}
	for _, tt := range tests {
		if got := ntfyPriority(tt.category); got != tt.want {
			t.Errorf("%q: priority = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestNtfyMessageID(t *testing.T) {
	if got := ntfyMessageID([]byte(`{"id":"m_9","time":1}`)); got != "m_9" {
		t.Fatalf("id = %q", got)
	}
	if got := ntfyMessageID([]byte("not json")); got != "" {
		t.Fatalf("id = %q", got)
	}
}

func TestNtfyRejectsOtherKinds(t *testing.T) {
	transport, err := NewNtfy(Config{Client: testOutboundClient(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(context.Background(), Device{Kind: KindWebPush, Endpoint: "https://x"}, Message{})
	if OutcomeOf(err) != OutcomeNotDelivered {
		t.Fatalf("outcome = %v", OutcomeOf(err))
	}
}
