package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/config"
)

// allEnvVars is every variable Load reads. Tests set them all to "" so the
// developer's real environment can't leak in — "" means unset to Load.
var allEnvVars = []string{
	"EARNEST_HTTP_ADDR",
	"EARNEST_CONTENT_ADDR",
	"EARNEST_DATABASE_URL",
	"EARNEST_BASE_URL",
	"EARNEST_CONTENT_ORIGIN",
	"EARNEST_SECRET_KEY",
	"EARNEST_OUTBOUND_ALLOW_PRIVATE",
	"EARNEST_TRUSTED_PROXIES",
	"EARNEST_LOG_LEVEL",
	"EARNEST_FETCH_MIN_INTERVAL",
	"EARNEST_FETCH_MAX_INTERVAL",
	"EARNEST_RETENTION_DAYS",
	"EARNEST_PUSH_VAPID_PRIVATE_KEY", "EARNEST_PUSH_VAPID_PUBLIC_KEY",
	"EARNEST_PUSH_VAPID_SUBJECT", "EARNEST_PUSH_VAPID_KEY_FILE",
	"EARNEST_APNS_PRIVATE_KEY", "EARNEST_APNS_KEY_ID", "EARNEST_APNS_TEAM_ID", "EARNEST_APNS_SANDBOX",
}

// requiredEnv is the minimum viable environment.
var requiredEnv = map[string]string{
	"EARNEST_DATABASE_URL": "postgres://earnest:pw@db.internal:5432/earnest",
	"EARNEST_SECRET_KEY":   "test-key-material-of-32-bytes-padded",
}

func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	for _, name := range allEnvVars {
		t.Setenv(name, "")
	}
	for name, value := range requiredEnv {
		t.Setenv(name, value)
	}
	for name, value := range overrides {
		t.Setenv(name, value)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.Config{
		HTTPAddr:             ":8080",
		ContentAddr:          ":8081",
		DatabaseURL:          "postgres://earnest:pw@db.internal:5432/earnest",
		BaseURL:              "http://localhost:8080",
		ContentOrigin:        "http://localhost:8081",
		SecretKey:            []byte("test-key-material-of-32-bytes-padded"),
		OutboundAllowPrivate: nil,
		LogLevel:             slog.LevelInfo,
		FetchMinInterval:     15 * time.Minute,
		FetchMaxInterval:     24 * time.Hour,
		RetentionDays:        90,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load()\n got: %+v\nwant: %+v", got, want)
	}
}

func TestLoadValid(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		check func(t *testing.T, got config.Config)
	}{
		{
			name: "all fields set",
			env: map[string]string{
				"EARNEST_HTTP_ADDR":              "127.0.0.1:9090",
				"EARNEST_DATABASE_URL":           "host=db user=earnest dbname=earnest sslmode=disable",
				"EARNEST_BASE_URL":               "https://feeds.example.com",
				"EARNEST_CONTENT_ORIGIN":         "https://content.example.com",
				"EARNEST_SECRET_KEY":             "another-secret-key-material-32bytes!",
				"EARNEST_OUTBOUND_ALLOW_PRIVATE": " ollama.lan:11434 , 10.0.0.2:443 ",
				"EARNEST_TRUSTED_PROXIES":        " 172.18.0.1 , 10.0.0.0/8 ,::ffff:192.0.2.1",
				"EARNEST_LOG_LEVEL":              "debug",
				"EARNEST_FETCH_MIN_INTERVAL":     "5m",
				"EARNEST_FETCH_MAX_INTERVAL":     "12h",
				"EARNEST_RETENTION_DAYS":         "365",
			},
			check: func(t *testing.T, got config.Config) {
				want := config.Config{
					HTTPAddr:             "127.0.0.1:9090",
					ContentAddr:          ":8081",
					DatabaseURL:          "host=db user=earnest dbname=earnest sslmode=disable",
					BaseURL:              "https://feeds.example.com",
					ContentOrigin:        "https://content.example.com",
					SecretKey:            []byte("another-secret-key-material-32bytes!"),
					OutboundAllowPrivate: []string{"ollama.lan:11434", "10.0.0.2:443"},
					TrustedProxies:       []netip.Prefix{netip.MustParsePrefix("172.18.0.1/32"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.0.2.1/32")},
					LogLevel:             slog.LevelDebug,
					FetchMinInterval:     5 * time.Minute,
					FetchMaxInterval:     12 * time.Hour,
					RetentionDays:        365,
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Load()\n got: %+v\nwant: %+v", got, want)
				}
			},
		},
		{
			name: "postgresql URI scheme",
			env:  map[string]string{"EARNEST_DATABASE_URL": "postgresql://u:p@db/d"},
			check: func(t *testing.T, got config.Config) {
				if got.DatabaseURL != "postgresql://u:p@db/d" {
					t.Errorf("DatabaseURL = %q", got.DatabaseURL)
				}
			},
		},
		{
			name: "keyword DSN with quoted value",
			env:  map[string]string{"EARNEST_DATABASE_URL": "host=db password='s p a c e' user=earnest"},
			check: func(t *testing.T, got config.Config) {
				if got.DatabaseURL != "host=db password='s p a c e' user=earnest" {
					t.Errorf("DatabaseURL = %q", got.DatabaseURL)
				}
			},
		},
		{
			name: "trailing-slash base URL normalised",
			env:  map[string]string{"EARNEST_BASE_URL": "http://app.internal/"},
			check: func(t *testing.T, got config.Config) {
				if got.BaseURL != "http://app.internal" {
					t.Errorf("BaseURL = %q", got.BaseURL)
				}
			},
		},
		{
			name: "content origin differs only by port",
			env:  map[string]string{"EARNEST_CONTENT_ORIGIN": "http://localhost:9090"},
			check: func(t *testing.T, got config.Config) {
				if got.ContentOrigin != "http://localhost:9090" {
					t.Errorf("ContentOrigin = %q", got.ContentOrigin)
				}
			},
		},
		{
			name: "log level uppercase accepted",
			env:  map[string]string{"EARNEST_LOG_LEVEL": "WARN"},
			check: func(t *testing.T, got config.Config) {
				if got.LogLevel != slog.LevelWarn {
					t.Errorf("LogLevel = %v", got.LogLevel)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantEnv string // every error must name the offending variable
	}{
		{"missing database url", map[string]string{"EARNEST_DATABASE_URL": ""}, "EARNEST_DATABASE_URL"},
		{"database url garbage", map[string]string{"EARNEST_DATABASE_URL": "not a dsn"}, "EARNEST_DATABASE_URL"},
		{"database url wrong scheme", map[string]string{"EARNEST_DATABASE_URL": "mysql://db/d"}, "EARNEST_DATABASE_URL"},
		{"database url URI without host", map[string]string{"EARNEST_DATABASE_URL": "postgres:///earnest"}, "EARNEST_DATABASE_URL"},
		{"database url unterminated quote", map[string]string{"EARNEST_DATABASE_URL": "host=db password='x"}, "EARNEST_DATABASE_URL"},
		{"missing secret key", map[string]string{"EARNEST_SECRET_KEY": ""}, "EARNEST_SECRET_KEY"},
		{"secret key short", map[string]string{"EARNEST_SECRET_KEY": "only-31-bytes-xxxxxxxxxxxxxxxx"}, "EARNEST_SECRET_KEY"},
		{"http addr missing port", map[string]string{"EARNEST_HTTP_ADDR": "localhost"}, "EARNEST_HTTP_ADDR"},
		{"http addr nonnumeric port", map[string]string{"EARNEST_HTTP_ADDR": ":nope"}, "EARNEST_HTTP_ADDR"},
		{"http addr port zero", map[string]string{"EARNEST_HTTP_ADDR": ":0"}, "EARNEST_HTTP_ADDR"},
		{"http addr port too large", map[string]string{"EARNEST_HTTP_ADDR": ":70000"}, "EARNEST_HTTP_ADDR"},
		{"base url bad scheme", map[string]string{"EARNEST_BASE_URL": "ftp://app.example.com"}, "EARNEST_BASE_URL"},
		{"base url credentials", map[string]string{"EARNEST_BASE_URL": "http://u:p@app.example.com"}, "EARNEST_BASE_URL"},
		{"base url path", map[string]string{"EARNEST_BASE_URL": "http://app.example.com/v1"}, "EARNEST_BASE_URL"},
		{"base url invalid port", map[string]string{"EARNEST_BASE_URL": "http://app.example.com:99999"}, "EARNEST_BASE_URL"},
		{"content origin query", map[string]string{"EARNEST_CONTENT_ORIGIN": "http://localhost:8081/?q=1"}, "EARNEST_CONTENT_ORIGIN"},
		{"content origin fragment", map[string]string{"EARNEST_CONTENT_ORIGIN": "http://localhost:8081/#f"}, "EARNEST_CONTENT_ORIGIN"},
		{"content origin same as base", map[string]string{
			"EARNEST_BASE_URL":       "http://app.internal:8080",
			"EARNEST_CONTENT_ORIGIN": "http://app.internal:8080",
		}, "EARNEST_CONTENT_ORIGIN"},
		{"content origin same origin via default port", map[string]string{
			"EARNEST_BASE_URL":       "http://app.internal:80",
			"EARNEST_CONTENT_ORIGIN": "http://app.internal",
		}, "EARNEST_CONTENT_ORIGIN"},
		{"allow private interior empty", map[string]string{"EARNEST_OUTBOUND_ALLOW_PRIVATE": "a.lan:443,,b.lan:80"}, "EARNEST_OUTBOUND_ALLOW_PRIVATE"},
		{"trusted proxies hostname", map[string]string{"EARNEST_TRUSTED_PROXIES": "proxy.lan"}, "EARNEST_TRUSTED_PROXIES"},
		{"trusted proxies bad prefix", map[string]string{"EARNEST_TRUSTED_PROXIES": "10.0.0.0/33"}, "EARNEST_TRUSTED_PROXIES"},
		{"trusted proxies trailing comma", map[string]string{"EARNEST_TRUSTED_PROXIES": "172.18.0.1,"}, "EARNEST_TRUSTED_PROXIES"},
		{"allow private trailing comma", map[string]string{"EARNEST_OUTBOUND_ALLOW_PRIVATE": "a.lan:443,"}, "EARNEST_OUTBOUND_ALLOW_PRIVATE"},
		{"allow private missing port", map[string]string{"EARNEST_OUTBOUND_ALLOW_PRIVATE": "a.lan"}, "EARNEST_OUTBOUND_ALLOW_PRIVATE"},
		{"allow private nonnumeric port", map[string]string{"EARNEST_OUTBOUND_ALLOW_PRIVATE": "a.lan:abc"}, "EARNEST_OUTBOUND_ALLOW_PRIVATE"},
		{"allow private missing host", map[string]string{"EARNEST_OUTBOUND_ALLOW_PRIVATE": ":443"}, "EARNEST_OUTBOUND_ALLOW_PRIVATE"},
		{"allow private scheme in host", map[string]string{"EARNEST_OUTBOUND_ALLOW_PRIVATE": "http://a.lan:443"}, "EARNEST_OUTBOUND_ALLOW_PRIVATE"},
		{"log level unknown", map[string]string{"EARNEST_LOG_LEVEL": "verbose"}, "EARNEST_LOG_LEVEL"},
		{"fetch min not a duration", map[string]string{"EARNEST_FETCH_MIN_INTERVAL": "soon"}, "EARNEST_FETCH_MIN_INTERVAL"},
		{"fetch min negative", map[string]string{"EARNEST_FETCH_MIN_INTERVAL": "-5m"}, "EARNEST_FETCH_MIN_INTERVAL"},
		{"fetch max zero", map[string]string{"EARNEST_FETCH_MAX_INTERVAL": "0"}, "EARNEST_FETCH_MAX_INTERVAL"},
		{"fetch min exceeds max", map[string]string{
			"EARNEST_FETCH_MIN_INTERVAL": "2h",
			"EARNEST_FETCH_MAX_INTERVAL": "1h",
		}, "EARNEST_FETCH_MIN_INTERVAL"},
		{"retention not a number", map[string]string{"EARNEST_RETENTION_DAYS": "weekly"}, "EARNEST_RETENTION_DAYS"},
		{"retention zero", map[string]string{"EARNEST_RETENTION_DAYS": "0"}, "EARNEST_RETENTION_DAYS"},
		{"retention negative", map[string]string{"EARNEST_RETENTION_DAYS": "-30"}, "EARNEST_RETENTION_DAYS"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			got, err := config.Load()
			if err == nil {
				t.Fatalf("Load succeeded with %+v, want an %s error", got, tc.wantEnv)
			}
			if !strings.Contains(err.Error(), tc.wantEnv) {
				t.Fatalf("error %q does not name %s", err, tc.wantEnv)
			}
		})
	}
}

func TestLoadRetentionBounds(t *testing.T) {
	for _, tc := range []struct {
		days  string
		valid bool
	}{
		{"1", true},
		{"106751", true},
		{"106752", false},
		// These overflow to positive durations, bypassing the service's
		// negative-duration guard and expiring entries far too early.
		{"213504", false},
		{"213505", false},
		{"9223372036854775807", false},
	} {
		t.Run(tc.days, func(t *testing.T) {
			setEnv(t, map[string]string{"EARNEST_RETENTION_DAYS": tc.days})
			got, err := config.Load()
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "EARNEST_RETENTION_DAYS") {
					t.Fatalf("Load() = (%+v, %v), want retention validation error", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			duration := time.Duration(got.RetentionDays) * 24 * time.Hour
			if duration/(24*time.Hour) != time.Duration(got.RetentionDays) {
				t.Fatalf("retention duration overflowed: %d days became %s", got.RetentionDays, duration)
			}
		})
	}
}

func TestLoadErrorDoesNotLeakSecrets(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad DSN", map[string]string{"EARNEST_DATABASE_URL": "host=db password='hunter2"}},
		{"bad scheme DSN", map[string]string{"EARNEST_DATABASE_URL": "mysql://u:hunter2@db/d"}},
		{"short secret key", map[string]string{"EARNEST_SECRET_KEY": "hunter2"}},
		{"credentialed base url", map[string]string{"EARNEST_BASE_URL": "http://u:hunter2@app.internal"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			_, err := config.Load()
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Fatalf("error leaks secret material: %v", err)
			}
		})
	}
}

func TestConfigStringRedacts(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:             ":8080",
		ContentAddr:          ":8081",
		DatabaseURL:          "postgres://u:hunter2@db:5432/d",
		BaseURL:              "http://app.internal",
		ContentOrigin:        "http://content.internal",
		SecretKey:            []byte("hunter2-key-material-padded-32b"),
		OutboundAllowPrivate: []string{"ollama.lan:11434"},
		LogLevel:             slog.LevelWarn,
		FetchMinInterval:     15 * time.Minute,
		FetchMaxInterval:     24 * time.Hour,
		RetentionDays:        90,
	}

	s := cfg.String()
	if strings.Contains(s, "hunter2") {
		t.Fatalf("String leaks secret material: %s", s)
	}
	for _, want := range []string{
		"http_addr=:8080",
		"database_url=[redacted]",
		"base_url=http://app.internal",
		"content_origin=http://content.internal",
		"secret_key=[redacted]",
		"outbound_allow_private=ollama.lan:11434",
		"log_level=WARN",
		"fetch_min_interval=15m0s",
		"fetch_max_interval=24h0m0s",
		"retention_days=90",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("String missing %q in %s", want, s)
		}
	}
}

func TestRejectMalformedHostsAndEquivalentOrigins(t *testing.T) {
	for _, value := range []string{"http://:8080", "http://[example.com]:8080", "http://127.1:8080", "http://2130706433:8080", "http://app.internal:8080?"} {
		t.Run(value, func(t *testing.T) {
			setEnv(t, map[string]string{"EARNEST_BASE_URL": value})
			if _, err := config.Load(); err == nil {
				t.Fatal("malformed/ambiguous origin accepted")
			}
		})
	}
	setEnv(t, map[string]string{"EARNEST_BASE_URL": "http://[::1]:8080", "EARNEST_CONTENT_ORIGIN": "http://[0:0:0:0:0:0:0:1]:8080"})
	if _, err := config.Load(); err == nil {
		t.Fatal("equivalent IPv6 origins accepted")
	}
}

func TestLoggingRedacts(t *testing.T) {
	setEnv(t, nil)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	logger.Info("config", "config", cfg)
	for _, formatted := range []string{log.String(), fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		if strings.Contains(formatted, "pw@") || strings.Contains(formatted, "test-key-material") || strings.Contains(formatted, "116, 101, 115, 116") {
			t.Fatalf("configuration leaked credentials: %s", formatted)
		}
	}
}
