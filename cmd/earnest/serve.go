package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/L-K-M/Earnest/internal/api"
	"github.com/L-K-M/Earnest/internal/compat/greader"
	"github.com/L-K-M/Earnest/internal/config"
	"github.com/L-K-M/Earnest/internal/fetcher"
	"github.com/L-K-M/Earnest/internal/metrics"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/web"
)

const (
	// shutdownGrace bounds the HTTP drain after SIGINT or SIGTERM.
	shutdownGrace = 10 * time.Second
	// retentionInterval paces the change-log and entry retention sweeps.
	retentionInterval = 24 * time.Hour
)

// feedProber forwards Probe to the fetcher. service.New requires a prober
// while fetcher.New requires the service — a cycle. The delegate is assigned
// in serve before any goroutine or request can call Probe, so the forwarder
// resolves it without a setter on either side.
type feedProber struct {
	delegate service.FeedProber
}

// Probe implements service.FeedProber.
func (p *feedProber) Probe(ctx context.Context, rawURL string, creds *service.FeedCredentials) (string, service.FeedMeta, []service.IngestItem, error) {
	if p.delegate == nil {
		return "", service.FeedMeta{}, nil, service.ErrUnavailable
	}
	return p.delegate.Probe(ctx, rawURL, creds)
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	slog.SetLogLoggerLevel(cfg.LogLevel)
	logger := slog.Default()
	logger.Info("configuration", "config", cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := openDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	prober := &feedProber{}
	svc, err := service.New(db, service.Deps{FeedProber: prober, SecretKey: cfg.SecretKey})
	if err != nil {
		return fmt.Errorf("service: %w", err)
	}
	auth := service.NewAuth(db, time.Now, cfg.SecretKey)

	egress, err := outbound.New(outbound.Config{Version: version, AllowHosts: cfg.OutboundAllowPrivate})
	if err != nil {
		return fmt.Errorf("outbound client: %w", err)
	}

	registry := metrics.New()
	f, err := fetcher.New(svc, egress, fetcher.Config{
		MinInterval: cfg.FetchMinInterval,
		MaxInterval: cfg.FetchMaxInterval,
		ExtraPorts:  privatePorts(cfg.OutboundAllowPrivate),
		Metrics:     registry,
	})
	if err != nil {
		return fmt.Errorf("fetcher: %w", err)
	}
	prober.delegate = f

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.SecurityHeaders(buildMux(cfg.BaseURL, svc, auth, logger, registry)),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	go runRetention(ctx, svc, time.Duration(cfg.RetentionDays)*24*time.Hour, logger)

	fetchErr := make(chan error, 1)
	go func() {
		err := f.Run(ctx)
		fetchErr <- err
		// A dead scheduler leaves feeds stale: shut down so the process
		// supervision restarts the whole binary instead of serving on.
		if err != nil {
			logger.Error("fetcher failed", "error", err)
			stop()
		}
	}()

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	logger.Info("earnest listening", "addr", cfg.HTTPAddr, "version", version, "revision", revision)
	listenErr := srv.ListenAndServe()
	stop()
	<-shutdownDone

	if err := <-fetchErr; err != nil {
		return fmt.Errorf("fetcher: %w", err)
	}
	if listenErr != nil && listenErr != http.ErrServerClosed {
		return listenErr
	}
	return nil
}

// buildMux mounts the surfaces the binary serves. api.New already applies
// security headers and serves GET /openapi.json; the outer security-headers
// wrap extends the same policy to every other response.
func buildMux(baseURL string, svc *service.Service, auth *service.Auth, logger *slog.Logger, registry *metrics.Registry) *http.ServeMux {
	apiHandler := api.New(svc, auth, logger, baseURL)
	mux := http.NewServeMux()
	mux.Handle("/v1/", apiHandler)
	mux.Handle("/openapi.json", apiHandler)
	mux.Handle("/api/greader/", greader.New(svc, auth, logger))
	mux.Handle("/metrics", registry)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.Handle("/", web.New())
	return mux
}

// privatePorts lifts the ports of the allow-private destinations so feeds on
// operator-sanctioned tailnet services may use ports other than 80 and 443.
// The config already validated every entry as host:port.
func privatePorts(allow []string) []uint16 {
	var ports []uint16
	for _, entry := range allow {
		_, port, err := net.SplitHostPort(entry)
		if err != nil {
			continue
		}
		if n, err := strconv.Atoi(port); err == nil && n >= 1 && n <= 65535 {
			ports = append(ports, uint16(n))
		}
	}
	return ports
}

// runRetention prunes the change log and tombstones old entries on startup and
// then once per retentionInterval until the process stops. Sweeps share the
// serve context so shutdown abandons a pass instead of waiting on it.
func runRetention(ctx context.Context, svc *service.Service, retention time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		pruneRetention(ctx, svc, retention, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pruneRetention(ctx context.Context, svc *service.Service, retention time.Duration, logger *slog.Logger) {
	now := time.Now().UTC()
	if n, err := svc.RetainEntries(ctx, now, retention); err != nil {
		if ctx.Err() == nil {
			logger.Error("entry retention failed", "error", err)
		}
	} else if n > 0 {
		logger.Info("entries tombstoned by retention", "count", n)
	}
	if n, err := svc.RetainChanges(ctx, now, retention); err != nil {
		if ctx.Err() == nil {
			logger.Error("change retention failed", "error", err)
		}
	} else if n > 0 {
		logger.Info("change log pruned", "revisions", n)
	}
}
