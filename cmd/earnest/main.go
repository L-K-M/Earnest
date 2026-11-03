// Earnest is an RSS reader for web, Mac and Android, with an API that LLMs
// can use. It migrates the database before serving /healthz and reports its
// build version. The service layer adds the reader operations.
//
// The container image runs `earnest serve` and probes `earnest healthcheck`
// for liveness — the runtime image ships no curl on purpose.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/L-K-M/Earnest/internal/config"
	"github.com/L-K-M/Earnest/internal/store"
)

// Build metadata, stamped via -ldflags "-X main.version=… -X main.revision=…"
// by the Dockerfile, Makefile and scripts/build.sh. The defaults keep a bare
// `go build` honest about what it is.
var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			slog.Error("serve failed", "error", err)
			os.Exit(1)
		}
	case "migrate":
		if err := migrate(); err != nil {
			slog.Error("migrate failed", "error", err)
			os.Exit(1)
		}
	case "user", "token", "compat-password":
		if err := authCommand(os.Args[1:]); err != nil {
			slog.Error("identity command failed", "error", err)
			os.Exit(1)
		}
	case "generation":
		if err := rotateGeneration(os.Args[2:]); err != nil {
			slog.Error("generation rotation failed", "error", err)
			os.Exit(1)
		}
	case "healthcheck":
		os.Exit(healthcheck())
	case "version", "--version", "-version":
		fmt.Printf("earnest %s (%s)\n", version, revision)
	default:
		fmt.Fprintf(os.Stderr, "usage: earnest [serve|healthcheck|migrate|version|user|token|compat-password]\n")
		os.Exit(2)
	}
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	slog.SetLogLoggerLevel(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := openDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	addr := cfg.HTTPAddr

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	slog.Info("earnest listening", "addr", addr, "version", version, "revision", revision)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// migrate needs only database access, so maintenance does not require app secrets.
func migrate() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("EARNEST_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("EARNEST_DATABASE_URL is required")
	}
	db, err := openDatabase(ctx, dsn)
	if err != nil {
		return err
	}
	db.Close()
	slog.Info("database migrations complete")
	return nil
}

func openDatabase(ctx context.Context, dsn string) (*store.Store, error) {
	db, err := store.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"version":  version,
		"revision": revision,
	})
}

// healthcheck probes the running server's /healthz on loopback, deriving the
// port from EARNEST_HTTP_ADDR so it cannot drift from the listen port.
func healthcheck() int {
	addr := os.Getenv("EARNEST_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		fmt.Fprintf(os.Stderr, "healthcheck: cannot parse port from EARNEST_HTTP_ADDR=%q\n", addr)
		return 1
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// rotateGeneration invalidates restored cursors before the server starts.
func rotateGeneration(args []string) error {
	if len(args) != 1 || args[0] != "rotate" {
		return fmt.Errorf("usage: earnest generation rotate")
	}
	dsn := os.Getenv("EARNEST_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("EARNEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	generation, err := db.RotateGeneration(ctx)
	if err != nil {
		return fmt.Errorf("rotate generation: %w", err)
	}
	fmt.Println(generation)
	return nil
}
