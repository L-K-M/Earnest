// Package clientip derives the rate-limiting client key for an HTTP request.
package clientip

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Trusted lists the peers whose X-Forwarded-For entries are believed: the
// reverse proxies in front of Earnest. Any other peer is identified by its
// socket address, and its forwarding headers are attacker-controlled and
// ignored.
type Trusted []netip.Prefix

// Loopback trusts only same-host proxies such as `tailscale serve` in front of
// a bare binary. Configured proxies extend it (see WithProxies).
var Loopback = Trusted{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}

// WithProxies returns the loopback set extended by the operator's proxies, for
// example the Docker network gateway that a proxy on the host connects from
// when Earnest runs in Compose.
func WithProxies(proxies []netip.Prefix) Trusted {
	return append(append(Trusted{}, Loopback...), proxies...)
}

func (t Trusted) contains(ip netip.Addr) bool {
	for _, prefix := range t {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// key derives the client identity for r.
//
// A trusted peer is a reverse proxy, and proxies append the address they
// observed to the right of X-Forwarded-For, so the rightmost entry that is not
// itself a trusted proxy is the client the proxy actually saw; entries to its
// left are client-chosen. A process on a trusted host can therefore choose its
// bucket, which is why the set should name only the proxies in front of
// Earnest. The second result is false when no usable client address exists;
// callers must then fall back to a per-credential key (which trades a shared
// lockout for a per-account one) instead of bucketing all clients together.
func (t Trusted) key(r *http.Request) (string, bool) {
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
	if !t.contains(ip) {
		return ip.String(), true
	}
	// Repeated X-Forwarded-For header lines are one comma-joined list, so
	// combine them in wire order before scanning from the right.
	forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if forwarded == "" {
		return "", false
	}
	entries := strings.Split(forwarded, ",")
	for i := len(entries) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(entries[i]))
		if err != nil {
			return "", false
		}
		addr = addr.Unmap()
		if addr.IsUnspecified() {
			return "", false
		}
		if t.contains(addr) {
			continue // another trusted proxy hop
		}
		return addr.String(), true
	}
	return "", false
}

type contextKey struct{}

type resolved struct {
	key string
	ok  bool
}

// Middleware resolves each request's client key against the trusted proxies
// once, so handlers read it through Key without knowing the configuration.
func Middleware(trusted Trusted, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := trusted.key(r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, resolved{key: key, ok: ok})))
	})
}

// Key returns the identity a rate limiter should bucket this request under:
// the key Middleware resolved, or, for a handler mounted without it, the key
// under the loopback-only default.
func Key(r *http.Request) (string, bool) {
	if v, ok := r.Context().Value(contextKey{}).(resolved); ok {
		return v.key, v.ok
	}
	return Loopback.key(r)
}
