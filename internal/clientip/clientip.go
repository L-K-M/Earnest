// Package clientip derives the rate-limiting client key for an HTTP request.
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Key returns the identity a rate limiter should bucket this request under.
//
// A loopback peer is a trusted reverse proxy running on the same host (for
// example `tailscale serve`, the documented HTTPS front end), so the first
// X-Forwarded-For entry names the real client. Any other peer is identified
// by its socket address; its forwarding headers are attacker-controlled and
// ignored. The second result is false when no usable client address exists;
// callers must then fall back to a per-credential key instead of sharing one
// bucket across all clients.
func Key(r *http.Request) (string, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// The socket address is not an IP literal; do not bucket on a shared
		// string, let the caller fall back to a per-credential key.
		return "", false
	}
	if ip.IsLoopback() {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if addr, err := netip.ParseAddr(strings.TrimSpace(first)); err == nil && !addr.IsUnspecified() {
				return addr.String(), true
			}
		}
		return "", false
	}
	return ip.String(), true
}
