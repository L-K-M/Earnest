package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestBearerTokenRequiresSecureTransport(t *testing.T) {
	c := newClient("http://reader.example", "private-token")
	_, err := c.get(context.Background(), "/v1/me", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("remote HTTP credential request: %v", err)
	}
}

func TestBearerTokenDoesNotFollowCleartextRedirect(t *testing.T) {
	c := newClient("https://reader.example", "private-token")
	request, _ := http.NewRequest(http.MethodGet, "http://reader.example/v1/me", nil)
	if err := c.http.CheckRedirect(request, nil); err == nil {
		t.Fatal("accepted HTTPS to HTTP redirect")
	}
}

func TestBearerTokenDoesNotFollowCrossOriginRedirect(t *testing.T) {
	c := newClient("https://reader.example", "private-token")
	first, _ := http.NewRequest(http.MethodGet, "https://reader.example/v1/me", nil)
	redirect, _ := http.NewRequest(http.MethodGet, "https://other.reader.example/v1/me", nil)
	if err := c.http.CheckRedirect(redirect, []*http.Request{first}); err == nil {
		t.Fatal("accepted cross-origin redirect")
	}
}
