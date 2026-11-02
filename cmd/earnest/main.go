// Earnest is an RSS reader for web, Mac and Android, with an API that LLMs
// can use. This binary is the deployment scaffold: it serves /healthz and
// reports its build version so the Docker, Compose and update chain is real
// before the service layer lands (docs/research/ sequence, step 2).
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
	case "healthcheck":
		os.Exit(healthcheck())
	case "version", "--version", "-version":
		fmt.Printf("earnest %s (%s)\n", version, revision)
	default:
		fmt.Fprintf(os.Stderr, "usage: earnest [serve|healthcheck|version]\n")
		os.Exit(2)
	}
}

func serve() error {
	addr := os.Getenv("EARNEST_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// Wired by compose today, unused by the scaffold — the service layer picks
	// it up when it lands. Reported (not hidden) so a missing value is visible.
	db := "not configured"
	if os.Getenv("EARNEST_DATABASE_URL") != "" {
		db = "configured"
	}
	slog.Info("earnest listening", "addr", addr, "version", version, "revision", revision, "database", db)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
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
