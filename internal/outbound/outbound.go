// Package outbound provides the shared, address-checked HTTP egress client.
// Callers must independently enforce which principals may initiate a fetch.
package outbound

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const UserAgentFormat = "Earnest/%s (+https://github.com/L-K-M/Earnest)"
const defaultRatio = 100
const defaultSize int64 = 10 << 20
const defaultTimeout = 30 * time.Second

type Purpose string

const (
	Feed      Purpose = "feed"
	Discovery Purpose = "discovery"
	Extract   Purpose = "extract"
	Favicon   Purpose = "favicon"
	Image     Purpose = "image"
	Push      Purpose = "push"
	Webhook   Purpose = "webhook"
	LLM       Purpose = "llm"
)

type RedirectPolicy uint8

const (
	RedirectFollow RedirectPolicy = iota
	RedirectNone
)

// Protocol selects the permitted HTTP protocol.
type Protocol uint8

const (
	HTTP1 Protocol = iota
	HTTP2
)

type Credentials struct {
	Username, Password string
	Headers            http.Header
}
type Policy struct {
	Purpose      Purpose
	Protocol     Protocol
	Credentials  Credentials
	MaxBodySize  int64
	Timeout      time.Duration
	Redirect     RedirectPolicy
	ExtraPorts   []uint16
	ETag         string
	LastModified string
}
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type systemResolver struct{}

func (systemResolver) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	v4, err4 := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	v6, err6 := net.DefaultResolver.LookupNetIP(ctx, "ip6", host)
	for i := range v4 {
		v4[i] = v4[i].Unmap()
	}
	if len(v4) == 0 && len(v6) == 0 {
		if err4 != nil {
			return nil, err4
		}
		return nil, err6
	}
	return append(v4, v6...), nil
}

type Config struct {
	Version string
	// AllowHosts contains exact host:port entries; AllowPrefixes permits addresses in a prefix.
	AllowHosts    []string
	AllowPrefixes []netip.Prefix
	// Spacing applies to all purposes. Zero selects the two-second default.
	Spacing  time.Duration
	Resolver Resolver
}
type Client struct {
	config Config
	mu     sync.Mutex
	hosts  map[string]*hostLimiter
	// One transport per protocol. A single pool keeps the connection count
	// and the transport objects bounded no matter how many destinations the
	// fetcher sees. The dial-time address check still runs whenever a new
	// connection is made; a pooled connection is already bound to the
	// validated address.
	transports map[Protocol]*http.Transport
}
type hostLimiter struct {
	gate chan struct{}
	next time.Time
}
type Result struct {
	Response          *http.Response
	FinalURL          *url.URL
	PermanentRedirect bool
}

func New(config Config) (*Client, error) {
	if config.Spacing < 0 {
		return nil, fmt.Errorf("negative spacing")
	}
	if config.Spacing == 0 {
		config.Spacing = 2 * time.Second
	}
	if config.Resolver == nil {
		config.Resolver = systemResolver{}
	}
	config.AllowHosts = append([]string(nil), config.AllowHosts...)
	config.AllowPrefixes = append([]netip.Prefix(nil), config.AllowPrefixes...)
	for _, h := range config.AllowHosts {
		if _, _, err := net.SplitHostPort(h); err != nil {
			return nil, fmt.Errorf("invalid allowed host %q: %w", h, err)
		}
	}
	for _, p := range config.AllowPrefixes {
		if !p.IsValid() {
			return nil, fmt.Errorf("invalid allowed prefix")
		}
	}
	c := &Client{config: config, hosts: make(map[string]*hostLimiter)}
	c.transports = map[Protocol]*http.Transport{
		HTTP1: c.newTransport(HTTP1),
		HTTP2: c.newTransport(HTTP2),
	}
	return c, nil
}

func (c *Client) Get(ctx context.Context, rawURL string, policy Policy) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req, policy)
}

func (c *Client) Do(request *http.Request, p Policy) (*Result, error) {
	if request == nil || request.URL == nil {
		return nil, fmt.Errorf("missing request or URL")
	}
	switch p.Purpose {
	case Feed, Discovery, Extract, Favicon, Image, Push, Webhook, LLM:
	default:
		return nil, fmt.Errorf("invalid purpose")
	}
	if p.Protocol != HTTP1 && p.Protocol != HTTP2 {
		return nil, fmt.Errorf("invalid protocol")
	}
	if p.Redirect != RedirectFollow && p.Redirect != RedirectNone {
		return nil, fmt.Errorf("invalid redirect policy")
	}
	if (p.Purpose == Push || p.Purpose == Webhook) && p.Redirect != RedirectNone {
		return nil, fmt.Errorf("push and webhook redirects must be disabled")
	}
	if p.MaxBodySize < 0 || p.MaxBodySize == 1<<63-1 || p.Timeout < 0 {
		return nil, fmt.Errorf("negative limit")
	}
	if p.MaxBodySize == 0 {
		p.MaxBodySize = defaultSize
	}
	if p.Timeout == 0 {
		p.Timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(request.Context(), p.Timeout)
	defer cancel()
	req := request.Clone(ctx)
	req.Header = request.Header.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if req.URL.User != nil {
		return nil, fmt.Errorf("URL credentials are not allowed")
	}
	if p.ETag != "" {
		req.Header.Set("If-None-Match", p.ETag)
	}
	if p.LastModified != "" {
		req.Header.Set("If-Modified-Since", p.LastModified)
	}
	for k, v := range p.Credentials.Headers {
		req.Header.Del(k)
		for _, value := range v {
			req.Header.Add(k, value)
		}
	}
	req.Header.Set("User-Agent", fmt.Sprintf(UserAgentFormat, c.config.Version))
	req.Header.Set("Accept-Encoding", "gzip")
	if p.Credentials.Username != "" || p.Credentials.Password != "" {
		req.SetBasicAuth(p.Credentials.Username, p.Credentials.Password)
	}
	original := origin(req.URL)
	permanent := false
	seen := map[string]bool{}
	for hops := 0; ; hops++ {
		if err := validateURL(req.URL, p.ExtraPorts); err != nil {
			return nil, err
		}
		if seen[req.URL.String()] {
			return nil, classified(RedirectLoop, errors.New("redirect loop"))
		}
		seen[req.URL.String()] = true
		release, err := c.acquire(ctx, strings.ToLower(req.URL.Hostname()))
		if err != nil {
			return nil, classifyNetwork(err)
		}
		transport := c.transport(p.Protocol)
		resp, err := transport.RoundTrip(req)
		if err != nil {
			release()
			return nil, classifyNetwork(err)
		}
		if isRedirect(resp.StatusCode) && p.Redirect == RedirectFollow && resp.Header.Get("Location") != "" {
			if hops >= 5 {
				resp.Body.Close()
				release()
				return nil, classified(RedirectLoop, errors.New("too many redirects"))
			}
			next, e := req.URL.Parse(resp.Header.Get("Location"))
			resp.Body.Close()
			release()
			if e != nil {
				return nil, fmt.Errorf("invalid redirect: %w", e)
			}
			if next.User != nil {
				return nil, fmt.Errorf("redirect URL credentials are not allowed")
			}
			if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusPermanentRedirect {
				permanent = true
			}
			nextReq := req.Clone(ctx)
			nextReq.URL = next
			nextReq.Host = ""
			nextReq.Header = req.Header.Clone()
			if origin(next) != original {
				for k := range nextReq.Header {
					sensitive := strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Proxy-Authorization") || strings.EqualFold(k, "Cookie")
					for credential := range p.Credentials.Headers {
						if strings.EqualFold(k, credential) {
							sensitive = true
						}
					}
					if sensitive {
						delete(nextReq.Header, k)
					}
				}
			}
			if resp.StatusCode == http.StatusSeeOther || ((resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound) && req.Method == http.MethodPost) {
				nextReq.Method = http.MethodGet
				nextReq.Body = nil
				nextReq.ContentLength = 0
			} else if req.Body != nil {
				if req.GetBody == nil {
					return nil, fmt.Errorf("cannot replay request body on redirect")
				}
				nextReq.Body, err = req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("replay request: %w", err)
				}
			}
			req = nextReq
			continue
		}
		data, err := readBody(resp, p.MaxBodySize)
		resp.Body.Close()
		release()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(data))
		resp.ContentLength = int64(len(data))
		result := &Result{Response: resp, FinalURL: req.URL, PermanentRedirect: permanent}
		if e := statusError(resp); e != nil {
			return result, e
		}
		return result, nil
	}
}
func origin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}
func isRedirect(code int) bool {
	return code == 301 || code == 302 || code == 303 || code == 307 || code == 308
}
func validateURL(u *url.URL, extra []uint16) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return classified(DeniedAddress, fmt.Errorf("unsupported scheme"))
	}
	if u.Hostname() == "" || u.User != nil {
		return classified(DeniedAddress, fmt.Errorf("invalid host"))
	}
	host := u.Hostname()
	if strings.Trim(host, "0123456789.") == "" {
		if _, err := netip.ParseAddr(host); err != nil {
			return classified(DeniedAddress, fmt.Errorf("invalid numeric address"))
		}
	}
	if strings.Contains(u.Hostname(), "%") {
		return classified(DeniedAddress, fmt.Errorf("zoned address"))
	}
	port := u.Port()
	if port == "" {
		return nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return classified(DeniedAddress, fmt.Errorf("invalid port"))
	}
	if n == 80 || n == 443 {
		return nil
	}
	for _, allowed := range extra {
		if int(allowed) == n {
			return nil
		}
	}
	return classified(DeniedAddress, fmt.Errorf("port not allowed"))
}

var denied = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "198.18.0.0/15", "240.0.0.0/4", "::/128", "::1/128", "::/96", "fc00::/7", "fe80::/10", "::ffff:0:0/96", "64:ff9b::/96"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func (c *Client) allowed(addr netip.Addr, hostport string) bool {
	if !addr.IsValid() || addr.Zone() != "" || addr.IsUnspecified() {
		return false
	}
	blocked := false
	for _, p := range denied {
		if p.Contains(addr) {
			blocked = true
			break
		}
	}
	if !blocked {
		return true
	}
	for _, h := range c.config.AllowHosts {
		if strings.EqualFold(h, hostport) {
			return true
		}
	}
	for _, p := range c.config.AllowPrefixes {
		if p.Contains(addr) || (addr.Is4In6() && p.Contains(addr.Unmap())) {
			return true
		}
	}
	return false
}
func (c *Client) transport(protocol Protocol) *http.Transport {
	return c.transports[protocol]
}

// newTransport builds one connection pool for a protocol. The destination
// host:port arrives in the dial address, so the allowlist can still be
// applied to the name the caller asked for. Compression stays disabled
// because Do sets Accept-Encoding and readBody decompresses manually;
// keep-alives stay enabled so the pool is useful.
func (c *Client) newTransport(protocol Protocol) *http.Transport {
	protocols := new(http.Protocols)
	if protocol == HTTP2 {
		protocols.SetHTTP2(true)
	} else {
		protocols.SetHTTP1(true)
	}
	return &http.Transport{Protocols: protocols, Proxy: nil, DisableCompression: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		hostport := address
		ips, err := c.config.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, classified(Timeout, err)
			}
			return nil, classified(DNS, err)
		}
		if len(ips) == 0 {
			return nil, classified(DNS, errors.New("no addresses"))
		}
		var last error
		for _, ip := range ips {
			if !c.allowed(ip, hostport) {
				return nil, classified(DeniedAddress, fmt.Errorf("address denied: %s", ip))
			}
			// Dual-stack sockets may report an IPv4 target as IPv4-mapped IPv6.
			dialer := &net.Dialer{Control: func(_, address string, _ syscall.RawConn) error {
				h, _, err := net.SplitHostPort(address)
				if err != nil {
					return classified(DeniedAddress, err)
				}
				actual, err := netip.ParseAddr(h)
				if err != nil {
					return classified(DeniedAddress, err)
				}
				if ip.Is4() && actual.Is4In6() {
					actual = actual.Unmap()
				}
				if actual != ip || !c.allowed(actual, hostport) {
					return classified(DeniedAddress, fmt.Errorf("address denied: %s", h))
				}
				return nil
			}}
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}}
}
func (c *Client) acquire(ctx context.Context, host string) (func(), error) {
	c.mu.Lock()
	h := c.hosts[host]
	if h == nil {
		h = &hostLimiter{gate: make(chan struct{}, 1)}
		c.hosts[host] = h
	}
	c.mu.Unlock()
	select {
	case h.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	wait := time.Until(h.next)
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			<-h.gate
			return nil, ctx.Err()
		}
	}
	h.next = time.Now().Add(c.config.Spacing)
	return func() { <-h.gate }, nil
}
func readBody(resp *http.Response, limit int64) ([]byte, error) {
	raw := &countReader{r: resp.Body}
	var reader io.Reader = raw
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		if !strings.EqualFold(encoding, "gzip") {
			return nil, classified(Decode, fmt.Errorf("unsupported content encoding"))
		}
		gz, err := gzip.NewReader(raw)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, classified(Timeout, err)
			}
			return nil, classified(Decode, err)
		}
		defer gz.Close()
		reader = gz
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, classified(Timeout, err)
		}
		return nil, classified(Decode, err)
	}
	if int64(len(data)) > limit || (resp.Header.Get("Content-Encoding") != "" && int64(len(data)) > raw.n*defaultRatio) {
		return nil, classified(TooLarge, errors.New("response exceeds limit"))
	}
	return data, nil
}

type countReader struct {
	r io.Reader
	n int64
}

func (r *countReader) Read(p []byte) (int, error) { n, e := r.r.Read(p); r.n += int64(n); return n, e }

type Class string

const (
	DNS           Class = "dns"
	Connect       Class = "connect"
	TLS           Class = "tls"
	Timeout       Class = "timeout"
	DeniedAddress Class = "denied_address"
	TooLarge      Class = "too_large"
	HTTPGone      Class = "http_4xx_gone"
	HTTP429       Class = "http_429"
	HTTP403       Class = "http_403"
	HTTP5xx       Class = "http_5xx"
	RedirectLoop  Class = "redirect_loop"
	Decode        Class = "decode"
)

type Error struct {
	Class      Class
	Err        error
	RetryAfter time.Duration
}

func (e *Error) Error() string            { return string(e.Class) + ": " + e.Err.Error() }
func (e *Error) Unwrap() error            { return e.Err }
func classified(c Class, err error) error { return &Error{Class: c, Err: err} }
func ErrorClass(err error) Class {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return ""
}
func classifyNetwork(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return classified(Timeout, err)
	}
	var n net.Error
	if errors.As(err, &n) && n.Timeout() {
		return classified(Timeout, err)
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return classified(DNS, err)
	}
	var tlsErr tls.RecordHeaderError
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || errors.As(err, &certErr) {
		return classified(TLS, err)
	}
	return classified(Connect, err)
}
func statusError(r *http.Response) error {
	var c Class
	switch {
	case r.StatusCode == 404 || r.StatusCode == 410:
		c = HTTPGone
	case r.StatusCode == 429:
		c = HTTP429
	case r.StatusCode == 403:
		c = HTTP403
	case r.StatusCode >= 500 && r.StatusCode <= 599:
		c = HTTP5xx
	default:
		return nil
	}
	d, _ := ParseRetryAfter(r.Header.Get("Retry-After"), time.Now())
	return &Error{Class: c, Err: fmt.Errorf("HTTP %s", r.Status), RetryAfter: d}
}

// ParseRetryAfter returns a nonnegative delay relative to now.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= 0 && n <= int64((1<<63-1)/int64(time.Second)) {
		return time.Duration(n) * time.Second, true
	}
	t, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	d := t.Sub(now)
	if d < 0 {
		d = 0
	}
	return d, true
}

// ParseMaxAge extracts a valid Cache-Control max-age directive.
func ParseMaxAge(value string) (time.Duration, bool) {
	for _, part := range strings.Split(value, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "max-age") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), "\"")
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 || n > int64((1<<63-1)/int64(time.Second)) {
			return 0, false
		}
		return time.Duration(n) * time.Second, true
	}
	return 0, false
}
