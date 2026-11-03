package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/L-K-M/Earnest/internal/api"
	"github.com/L-K-M/Earnest/internal/compat/greader"
	"github.com/L-K-M/Earnest/internal/config"
	"github.com/L-K-M/Earnest/internal/fetcher"
	"github.com/L-K-M/Earnest/internal/imageproxy"
	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/mcp"
	"github.com/L-K-M/Earnest/internal/metrics"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/web"
)

const (
	// shutdownGrace bounds the HTTP drain after SIGINT or SIGTERM.
	shutdownGrace = 10 * time.Second
	// retentionInterval paces the change-log and entry retention sweeps.
	retentionInterval = 24 * time.Hour
	deliveryInterval  = time.Second
	deliveryBatchSize = 100
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

	egress, err := outbound.New(outbound.Config{Version: version, AllowHosts: cfg.OutboundAllowPrivate})
	if err != nil {
		return fmt.Errorf("outbound client: %w", err)
	}
	llmConfig, err := llm.Load()
	if err != nil {
		return fmt.Errorf("model configuration: %w", err)
	}
	router, err := llm.NewRouter(llmConfig, egress)
	if err != nil {
		return fmt.Errorf("model router: %w", err)
	}
	modelBudget := llm.NewBudget(db, llmConfig.Limits())
	modelClient, err := llm.NewClient(router, modelBudget, llmConfig.Models)
	if err != nil {
		return fmt.Errorf("model client: %w", err)
	}
	engine, err := pipeline.New(router, modelClient, llm.NewCache(db), llmConfig.Models)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	ctx = service.WithPipeline(ctx, engine)
	if batch := pipeline.NewEditionBatch(router, modelBudget, llmConfig, egress); batch != nil {
		ctx = pipeline.WithEditionBatch(ctx, batch)
	}
	pushRegistry, publicKey, err := buildPushRegistry(cfg, egress)
	if err != nil {
		return fmt.Errorf("push transports: %w", err)
	}

	prober := &feedProber{}
	svc, err := service.New(db, service.Deps{FeedProber: prober, SecretKey: cfg.SecretKey, PushRegistry: pushRegistry, Outbound: egress})
	if err != nil {
		return fmt.Errorf("service: %w", err)
	}
	auth := service.NewAuth(db, time.Now, cfg.SecretKey)
	// Interrupted sends have an unknown outcome and must never be retried.
	if _, err := svc.RecoverNotificationAttempts(ctx); err != nil {
		return fmt.Errorf("recover notification attempts: %w", err)
	}

	proxy, err := imageproxy.New(cfg.SecretKey, egress, cfg.ContentOrigin)
	if err != nil {
		return fmt.Errorf("image proxy: %w", err)
	}
	content, err := web.NewContent(svc, auth, proxy, cfg.ContentOrigin, cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("content: %w", err)
	}
	contentMux := http.NewServeMux()
	contentMux.Handle("/content/", content)
	contentMux.Handle("/img/", proxy)
	contentServer := &http.Server{Addr: cfg.ContentAddr, Handler: contentMux, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}

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
		Handler:           api.SecurityHeaders(buildMuxWithWeb(cfg.BaseURL, cfg.ContentOrigin, svc, auth, logger, registry, content, publicKey)),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	contentErr := make(chan error, 1)
	go func() {
		err := contentServer.ListenAndServe()
		contentErr <- err
		if err != nil && err != http.ErrServerClosed {
			logger.Error("content listener failed", "error", err)
			stop()
		}
	}()

	go runRetention(ctx, svc, time.Duration(cfg.RetentionDays)*24*time.Hour, logger)

	var workers sync.WaitGroup
	for _, job := range []struct {
		name string
		run  func(context.Context, int) (int, error)
	}{
		{"notification delivery", svc.DeliverNotifications},
		{"webhook delivery", svc.RunWebhookJobs},
		{"rule jobs", svc.RunRuleJobs},
		{"edition jobs", svc.RunEditionJobs},
	} {
		workers.Go(func() { runDelivery(ctx, logger, job.name, job.run) })
	}
	if router.Enabled(llm.TaskEmbed) || router.Enabled(llm.TaskClassify) {
		workers.Go(func() { runDelivery(ctx, logger, "internal pipeline", svc.RunPipeline) })
	}
	// Stop and join delivery before the deferred database close.
	defer workers.Wait()
	defer stop()

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
		_ = contentServer.Shutdown(ctx)
	}()

	logger.Info("earnest listening", "addr", cfg.HTTPAddr, "version", version, "revision", revision)
	listenErr := srv.ListenAndServe()
	stop()
	<-shutdownDone

	if err := <-contentErr; err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("content listener: %w", err)
	}
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
func buildMux(baseURL string, svc *service.Service, auth *service.Auth, logger *slog.Logger, registry *metrics.Registry, vapidPublicKey ...string) *http.ServeMux {
	return buildMuxWithWeb(baseURL, "http://localhost:8081", svc, auth, logger, registry, nil, vapidPublicKey...)
}

func buildMuxWithWeb(baseURL, contentOrigin string, svc *service.Service, auth *service.Auth, logger *slog.Logger, registry *metrics.Registry, content *web.Content, vapidPublicKey ...string) *http.ServeMux {
	apiHandler := api.New(svc, auth, logger, baseURL, content, vapidPublicKey...)
	mux := http.NewServeMux()
	mux.Handle("/v1/", apiHandler)
	mux.Handle("/openapi.json", apiHandler)
	mux.Handle("/api/greader/", greader.New(svc, auth, logger))
	mux.Handle("/mcp", mcp.New(svc, auth, baseURL, logger))
	mux.HandleFunc("/llms.txt", mcp.LLMS)
	mux.Handle("/metrics", registry)
	mux.HandleFunc("/healthz", healthzHandler)
	if content != nil {
		mux.HandleFunc("GET /web/content-url/{entry}", content.URLHandler)
	}
	mux.Handle("/", web.New(contentOrigin))
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

// runDelivery bounds each pass and never overlaps attempts within a worker.
func runDelivery(ctx context.Context, logger *slog.Logger, name string, run func(context.Context, int) (int, error)) {
	ticker := time.NewTicker(deliveryInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := run(ctx, deliveryBatchSize); err != nil && ctx.Err() == nil {
			logger.Error(name+" failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func buildPushRegistry(cfg config.Config, egress *outbound.Client) (*push.Registry, string, error) {
	keys, err := loadVAPIDKeys(cfg)
	if err != nil {
		return nil, "", err
	}
	transports := push.Config{Client: egress, ExtraPorts: privatePorts(cfg.OutboundAllowPrivate)}
	ntfy, err := push.NewNtfy(transports)
	if err != nil {
		return nil, "", fmt.Errorf("ntfy: %w", err)
	}
	webPush, err := push.NewWebPush(transports, keys)
	if err != nil {
		return nil, "", fmt.Errorf("Web Push: %w", err)
	}
	unified, err := push.NewUnifiedPush(transports, &keys)
	if err != nil {
		return nil, "", fmt.Errorf("UnifiedPush: %w", err)
	}
	registry := push.NewRegistry()
	for _, item := range []struct {
		kind      push.Kind
		transport push.Transport
	}{
		{push.KindNtfy, ntfy}, {push.KindWebPush, webPush}, {push.KindUnifiedPush, unified},
	} {
		if err := registry.Register(item.kind, item.transport); err != nil {
			return nil, "", err
		}
	}
	if len(cfg.APNsPrivateKey) > 0 {
		apns, err := push.NewAPNs(push.APNsConfig{Client: egress, PrivateKey: cfg.APNsPrivateKey, KeyID: cfg.APNsKeyID, TeamID: cfg.APNsTeamID, Sandbox: cfg.APNsSandbox})
		if err != nil {
			return nil, "", fmt.Errorf("APNs: %w", err)
		}
		if err := registry.Register(push.KindAPNs, apns); err != nil {
			return nil, "", err
		}
	}
	return registry, keys.PublicKey, nil
}

// loadVAPIDKeys publishes a complete file atomically without replacing a pair
// another process already created. Losing that pair invalidates subscriptions.
func loadVAPIDKeys(cfg config.Config) (push.VAPIDKeys, error) {
	if len(cfg.PushVAPIDPrivateKey) > 0 {
		return push.VAPIDKeys{PrivateKey: cfg.PushVAPIDPrivateKey, PublicKey: cfg.PushVAPIDPublicKey, Subject: cfg.PushVAPIDSubject}, nil
	}
	path := cfg.PushVAPIDKeyFile
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return push.VAPIDKeys{}, fmt.Errorf("VAPID config directory: %w", err)
		}
		path = filepath.Join(dir, "earnest", "vapid.json")
	}
	read := func() (push.VAPIDKeys, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return push.VAPIDKeys{}, err
		}
		var keys push.VAPIDKeys
		if err := json.Unmarshal(data, &keys); err != nil {
			return push.VAPIDKeys{}, fmt.Errorf("decode VAPID key file: %w", err)
		}
		keys.Subject = cfg.PushVAPIDSubject
		return keys, nil
	}
	keys, err := read()
	if err == nil {
		return keys, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return push.VAPIDKeys{}, fmt.Errorf("read VAPID keys: %w", err)
	}
	keys, err = push.GenerateVAPIDKeys(cfg.PushVAPIDSubject)
	if err != nil {
		return push.VAPIDKeys{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return push.VAPIDKeys{}, fmt.Errorf("VAPID key directory: %w", err)
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return push.VAPIDKeys{}, fmt.Errorf("encode VAPID keys: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".vapid-*")
	if err != nil {
		return push.VAPIDKeys{}, fmt.Errorf("create VAPID key file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return push.VAPIDKeys{}, fmt.Errorf("write VAPID keys: %w", err)
	}
	if err := file.Sync(); err != nil {
		return push.VAPIDKeys{}, fmt.Errorf("sync VAPID keys: %w", err)
	}
	if err := file.Close(); err != nil {
		return push.VAPIDKeys{}, fmt.Errorf("close VAPID keys: %w", err)
	}
	if err := os.Link(file.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return read()
		}
		return push.VAPIDKeys{}, fmt.Errorf("publish VAPID keys: %w", err)
	}
	return keys, nil
}
