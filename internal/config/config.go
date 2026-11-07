// Package config loads and validates Earnest's environment configuration.
// Everything is checked once at startup so a misconfigured deployment fails
// before it opens a socket.
//
// Every variable is EARNEST_-prefixed, matching the deployment contract in
// compose.yaml and .env.example. Two are required: EARNEST_DATABASE_URL and
// EARNEST_SECRET_KEY.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	envContentAddr          = "EARNEST_CONTENT_ADDR"
	envHTTPAddr             = "EARNEST_HTTP_ADDR"
	envDatabaseURL          = "EARNEST_DATABASE_URL"
	envBaseURL              = "EARNEST_BASE_URL"
	envContentOrigin        = "EARNEST_CONTENT_ORIGIN"
	envSecretKey            = "EARNEST_SECRET_KEY"
	envOutboundAllowPrivate = "EARNEST_OUTBOUND_ALLOW_PRIVATE"
	envTrustedProxies       = "EARNEST_TRUSTED_PROXIES"
	envLogLevel             = "EARNEST_LOG_LEVEL"
	envFetchMinInterval     = "EARNEST_FETCH_MIN_INTERVAL"
	envFetchMaxInterval     = "EARNEST_FETCH_MAX_INTERVAL"
	envRetentionDays        = "EARNEST_RETENTION_DAYS"
)

const (
	defaultContentAddr   = ":8081"
	defaultHTTPAddr      = ":8080"
	defaultBaseURL       = "http://localhost:8080"
	defaultContentOrigin = "http://localhost:8081"
	defaultLogLevel      = "info"

	defaultFetchMinInterval = 15 * time.Minute
	defaultFetchMaxInterval = 24 * time.Hour
	defaultRetentionDays    = 90

	// minSecretKeyBytes is the smallest acceptable key length. The key signs
	// user-facing URLs and tokens, so short keys are rejected outright.
	minSecretKeyBytes = 32

	// redactedValue stands in for credentials in Config.String.
	redactedValue = "[redacted]"
)

// Config is Earnest's runtime configuration, populated by Load.
type Config struct {
	// HTTPAddr is the "host:port" listen address for the API server.
	HTTPAddr string

	// ContentAddr is the separate article listener address.
	ContentAddr string

	// DatabaseURL is the Postgres DSN: a postgres:// URI or a libpq-style
	// key=value string. It carries credentials and must never be logged.
	DatabaseURL string

	// BaseURL is the app's canonical origin, used to build absolute links.
	BaseURL string

	// ContentOrigin serves the sandboxed article HTML. It must be a
	// different origin than BaseURL or the sandbox isolates nothing.
	ContentOrigin string

	// SecretKey is raw key material (32+ bytes) for signing tokens and
	// image-proxy URLs. It must never be logged.
	SecretKey []byte

	// OutboundAllowPrivate lists "host:port" destinations the outbound
	// client may reach on private addresses (a local LLM, tailnet
	// webhooks). Everything else private is denied at dial time.
	OutboundAllowPrivate []string

	// TrustedProxies lists reverse proxies, beyond loopback, whose
	// X-Forwarded-For entries identify the client for login rate limits.
	TrustedProxies []netip.Prefix

	LogLevel         slog.Level
	FetchMinInterval time.Duration
	FetchMaxInterval time.Duration
	RetentionDays    int

	// PushVAPIDPrivateKey and PushVAPIDPublicKey override the persisted key pair.
	PushVAPIDPrivateKey []byte
	PushVAPIDPublicKey  string
	// PushVAPIDSubject is a mailto: or https: contact for push providers.
	PushVAPIDSubject string
	// PushVAPIDKeyFile defaults to earnest/vapid.json in the user config directory.
	// Keep this file on persistent storage when using a container.
	PushVAPIDKeyFile string
	// APNs is disabled unless all three key settings are supplied.
	APNsPrivateKey []byte
	APNsKeyID      string
	APNsTeamID     string
	APNsSandbox    bool
}

// GoString keeps Go-syntax formatting from exposing credentials.
func (c Config) GoString() string { return c.String() }

// LogValue keeps structured slog handlers from serializing secret fields.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

// String renders the config for startup logging with credentials redacted.
func (c Config) String() string {
	return fmt.Sprintf(
		"http_addr=%s content_addr=%s database_url=%s base_url=%s content_origin=%s secret_key=%s "+
			"outbound_allow_private=%s trusted_proxies=%s log_level=%s fetch_min_interval=%s "+
			"fetch_max_interval=%s retention_days=%d",
		c.HTTPAddr, c.ContentAddr, redactedValue, c.BaseURL, c.ContentOrigin, redactedValue,
		strings.Join(c.OutboundAllowPrivate, ","), prefixList(c.TrustedProxies), c.LogLevel,
		c.FetchMinInterval, c.FetchMaxInterval, c.RetentionDays)
}

// Load reads the environment into a validated Config. Errors name the
// offending variable but never quote the values of DATABASE_URL, SECRET_KEY
// or URLs, which may contain credentials.
func Load() (Config, error) {
	var cfg Config
	var err error

	if cfg.HTTPAddr, err = loadHTTPAddr(); err != nil {
		return Config{}, err
	}
	if cfg.ContentAddr, err = loadListenAddr(envContentAddr, defaultContentAddr); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseURL, err = loadDatabaseURL(); err != nil {
		return Config{}, err
	}
	if cfg.BaseURL, err = loadOrigin(envBaseURL, defaultBaseURL); err != nil {
		return Config{}, err
	}
	if cfg.ContentOrigin, err = loadOrigin(envContentOrigin, defaultContentOrigin); err != nil {
		return Config{}, err
	}
	if sameOrigin(cfg.BaseURL, cfg.ContentOrigin) {
		return Config{}, fmt.Errorf("%s must be a different origin than %s: "+
			"the content sandbox isolates nothing on the same origin",
			envContentOrigin, envBaseURL)
	}
	if cfg.SecretKey, err = loadSecretKey(); err != nil {
		return Config{}, err
	}
	if cfg.OutboundAllowPrivate, err = loadAllowPrivate(); err != nil {
		return Config{}, err
	}
	if cfg.TrustedProxies, err = loadTrustedProxies(); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = loadLogLevel(); err != nil {
		return Config{}, err
	}
	if cfg.FetchMinInterval, err = loadDuration(envFetchMinInterval, defaultFetchMinInterval); err != nil {
		return Config{}, err
	}
	if cfg.FetchMaxInterval, err = loadDuration(envFetchMaxInterval, defaultFetchMaxInterval); err != nil {
		return Config{}, err
	}
	if cfg.FetchMinInterval > cfg.FetchMaxInterval {
		return Config{}, fmt.Errorf("%s (%s) must not exceed %s (%s)",
			envFetchMinInterval, cfg.FetchMinInterval,
			envFetchMaxInterval, cfg.FetchMaxInterval)
	}
	if cfg.RetentionDays, err = loadRetentionDays(); err != nil {
		return Config{}, err
	}
	if err := cfg.loadPush(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// envOr returns the trimmed value of name, or def when unset or empty.
func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func loadHTTPAddr() (string, error) {
	return loadListenAddr(envHTTPAddr, defaultHTTPAddr)
}

func loadListenAddr(name, def string) (string, error) {
	addr := envOr(name, def)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("%s: invalid listen address %q: %v", name, addr, err)
	}
	// Port 0 would give an ephemeral listener the healthcheck can't find.
	if !validPort(port) {
		return "", fmt.Errorf("%s: invalid port %q", name, port)
	}
	return addr, nil
}

func loadDatabaseURL() (string, error) {
	dsn := envOr(envDatabaseURL, "")
	if dsn == "" {
		return "", fmt.Errorf("%s is required: a postgres:// URI or key=value DSN", envDatabaseURL)
	}
	if !validPostgresDSN(dsn) {
		return "", fmt.Errorf("%s is not a postgres:// URI or key=value DSN", envDatabaseURL)
	}
	return dsn, nil
}

// Use the driver parser so quoted passwords, escapes and pool parameters
// have exactly the same meaning here and when the store connects.
func validPostgresDSN(dsn string) bool {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil || u.Host == "" {
			return false
		}
	}
	_, err := pgxpool.ParseConfig(dsn)
	return err == nil
}

// loadOrigin parses a bare http(s) origin — scheme and host only, since the
// value joins origins for CSP, sandbox frames and absolute links. The raw
// input is never echoed in errors: a rejected value may hold credentials.
func loadOrigin(name, def string) (string, error) {
	u, err := url.Parse(envOr(name, def))
	if err != nil {
		return "", fmt.Errorf("%s is not a valid URL", name)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%s must use http or https", name)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%s must name a host", name)
	}
	if p := u.Port(); p != "" && !validPort(p) {
		return "", fmt.Errorf("%s has an invalid port", name)
	}
	if u.User != nil {
		return "", fmt.Errorf("%s must not contain credentials", name)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%s must be an origin without a path", name)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("%s must be an origin without query or fragment", name)
	}
	host, err := canonicalHost(u.Hostname())
	if err != nil {
		return "", fmt.Errorf("%s must name a valid ASCII host or IP address", name)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	return scheme + "://" + host, nil
}

// sameOrigin reports whether two origins loadOrigin normalised name the same
// origin, filling in default ports so "http://h" equals "http://h:80".
func sameOrigin(a, b string) bool {
	ua, _ := url.Parse(a) // a and b are already validated
	ub, _ := url.Parse(b)
	return ua.Scheme == ub.Scheme &&
		strings.EqualFold(ua.Hostname(), ub.Hostname()) &&
		effectivePort(ua) == effectivePort(ub)
}

func effectivePort(u *url.URL) int {
	if p := u.Port(); p != "" {
		n, _ := strconv.Atoi(p) // validated numeric by loadOrigin
		return n
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}

func loadSecretKey() ([]byte, error) {
	// Read raw, never trimmed: the env bytes are the key.
	key := os.Getenv(envSecretKey)
	if key == "" {
		return nil, fmt.Errorf("%s is required: %d+ random bytes", envSecretKey, minSecretKeyBytes)
	}
	if len(key) < minSecretKeyBytes {
		return nil, fmt.Errorf("%s must be at least %d bytes, got %d", envSecretKey, minSecretKeyBytes, len(key))
	}
	return []byte(key), nil
}

func loadAllowPrivate() ([]string, error) {
	raw := envOr(envOutboundAllowPrivate, "")
	if raw == "" {
		return nil, nil
	}
	var out []string
	for _, entry := range strings.Split(raw, ",") {
		e := strings.TrimSpace(entry)
		if e == "" {
			return nil, fmt.Errorf("%s: empty entry in host:port list", envOutboundAllowPrivate)
		}
		host, port, err := net.SplitHostPort(e)
		switch {
		case err != nil:
			return nil, fmt.Errorf("%s: invalid host:port entry", envOutboundAllowPrivate)
		case !validHost(host):
			return nil, fmt.Errorf("%s: entry has no valid host", envOutboundAllowPrivate)
		case !validPort(port):
			return nil, fmt.Errorf("%s: entry has an invalid port", envOutboundAllowPrivate)
		}
		out = append(out, e)
	}
	return out, nil
}

// loadTrustedProxies parses a comma-separated list of IP addresses and CIDR
// prefixes. An address is a single-host prefix.
func loadTrustedProxies() ([]netip.Prefix, error) {
	raw := envOr(envTrustedProxies, "")
	if raw == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, entry := range strings.Split(raw, ",") {
		e := strings.TrimSpace(entry)
		if e == "" {
			return nil, fmt.Errorf("%s: empty entry in address list", envTrustedProxies)
		}
		if strings.Contains(e, "/") {
			prefix, err := netip.ParsePrefix(e)
			if err != nil {
				return nil, fmt.Errorf("%s: invalid CIDR prefix %q", envTrustedProxies, e)
			}
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(e)
		if err != nil || addr.Zone() != "" {
			return nil, fmt.Errorf("%s: invalid IP address %q", envTrustedProxies, e)
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func prefixList(prefixes []netip.Prefix) string {
	parts := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		parts[i] = prefix.String()
	}
	return strings.Join(parts, ",")
}

func loadLogLevel() (slog.Level, error) {
	var level slog.Level
	s := envOr(envLogLevel, defaultLogLevel)
	// UnmarshalText only knows the uppercase names; uppercase first so
	// "info" works. It also accepts numeric offsets like "INFO+2".
	if err := level.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		return 0, fmt.Errorf("%s: unknown level %q (want debug, info, warn or error)", envLogLevel, s)
	}
	return level, nil
}

func loadDuration(name string, def time.Duration) (time.Duration, error) {
	s := envOr(name, "")
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", name, s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %q", name, s)
	}
	return d, nil
}

func loadRetentionDays() (int, error) {
	s := envOr(envRetentionDays, "")
	if s == "" {
		return defaultRetentionDays, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive number of days, got %q", envRetentionDays, s)
	}
	return n, nil
}

// validPort reports whether s is a numeric TCP port a listener or dial
// target can use: 1–65535, never 0.
func validPort(s string) bool {
	p, err := strconv.Atoi(s)
	return err == nil && p >= 1 && p <= 65535
}

// Canonical IP spelling matters for origin isolation: browsers normalize IPv6
// and legacy numeric IPv4 forms. Reject ambiguous numeric hosts and require
// ASCII DNS names (use punycode for international names).
func canonicalHost(host string) (string, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", fmt.Errorf("zoned address")
		}
		return ip.String(), nil
	}
	if host == "" || len(host) > 253 {
		return "", fmt.Errorf("invalid host")
	}
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid DNS label")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("invalid host character")
			}
		}
	}
	last := strings.ToLower(labels[len(labels)-1])
	if _, err := strconv.ParseUint(last, 10, 64); err == nil {
		return "", fmt.Errorf("ambiguous numeric host")
	}
	if strings.HasPrefix(last, "0x") {
		if _, err := strconv.ParseUint(last[2:], 16, 64); err == nil {
			return "", fmt.Errorf("ambiguous hexadecimal host")
		}
	}
	return strings.ToLower(host), nil
}

func validHost(host string) bool { _, err := canonicalHost(host); return err == nil }
