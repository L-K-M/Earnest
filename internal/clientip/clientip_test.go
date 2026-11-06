package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestKeyDirectPeer(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "203.0.113.7:51234"
	// A direct peer's forwarding header must be ignored.
	r.Header.Set("X-Forwarded-For", "198.51.100.9")
	key, ok := Key(r)
	if !ok || key != "203.0.113.7" {
		t.Fatalf("direct peer: got %q, %v", key, ok)
	}
}

func TestKeyLoopbackProxyForwarded(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")
	key, ok := Key(r)
	if !ok || key != "198.51.100.9" {
		t.Fatalf("forwarded: got %q, %v", key, ok)
	}
}

func TestKeyLoopbackProxyWithoutHeader(t *testing.T) {
	for _, remote := range []string{"127.0.0.1:8080", "[::1]:8080"} {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = remote
		if key, ok := Key(r); ok {
			t.Fatalf("loopback %q without header: got %q, true", remote, key)
		}
	}
}

func TestKeyNonIPRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "localhost"
	if key, ok := Key(r); ok {
		t.Fatalf("non-IP RemoteAddr: got %q, true", key)
	}
}

func TestKeyLoopbackProxyGarbageHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if _, ok := Key(r); ok {
		t.Fatal("garbage forwarded header must not yield a key")
	}
}

func TestKeyUnspecifiedForwarded(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "0.0.0.0")
	if _, ok := Key(r); ok {
		t.Fatal("unspecified forwarded address must not yield a key")
	}
}
