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
// A loopback peer is assumed to be the reverse proxy on an otherwise
// single-user host (for example `tailscale serve`, the documented HTTPS front
// end), so X-Forwarded-For is trusted only from loopback peers: any other
// local process could otherwise choose its bucket. Proxies append the address
// they observed to the right of X-Forwarded-For, so the rightmost non-loopback
// entry is the client the proxy actually saw; entries to its left are
// client-chosen. Any other peer is identified by its socket address; its
// forwarding headers are attacker-controlled and ignored. The second result
// is false when no usable client address exists; callers must then fall back
// to a per-credential key (which trades a shared lockout for a per-account
// one) instead of bucketing all clients together.
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
	ip = ip.Unmap()
	if ip.IsLoopback() {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			entries := strings.Split(forwarded, ",")
			for i := len(entries) - 1; i >= 0; i-- {
				addr, err := netip.ParseAddr(strings.TrimSpace(entries[i]))
				if err != nil || addr.IsUnspecified() {
					return "", false
				}
				addr = addr.Unmap()
				if addr.IsLoopback() {
					continue // another same-host proxy hop
				}
				return addr.String(), true
			}
		}
		return "", false
	}
	return ip.String(), true
}
