package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

const (
	buildTimeout      = 2 * time.Minute
	readinessTimeout  = time.Minute
	requestTimeout    = 10 * time.Second
	readinessInterval = 25 * time.Millisecond
	shutdownAllowance = 5 * time.Second
)

var (
	buildOnce  sync.Once
	binaryDir  string
	binaryPath string
	binaryErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binaryDir != "" {
		_ = os.RemoveAll(binaryDir)
	}
	os.Exit(code)
}

func shutdownBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		binaryDir, binaryErr = os.MkdirTemp("", "earnest-shutdown-test-")
		if binaryErr != nil {
			return
		}
		binaryPath = filepath.Join(binaryDir, "earnest")
		ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
		defer cancel()
		build := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, ".")
		build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-p=2")
		if output, err := build.CombinedOutput(); err != nil {
			binaryErr = fmt.Errorf("build: %w\n%s", err, output)
		}
	})
	if binaryErr != nil {
		t.Fatal(binaryErr)
	}
	return binaryPath
}

func TestServeShutdown(t *testing.T) {
	template := storetest.NewTemplate(t)
	dsn := template.DatabaseURL(t)
	binary := shutdownBinary(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "serve")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "EARNEST_") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, "EARNEST_DATABASE_URL="+dsn, "EARNEST_SECRET_KEY="+strings.Repeat("x", 32), "EARNEST_HTTP_ADDR="+addr, "EARNEST_BASE_URL=http://"+addr)
	var logs bytes.Buffer
	command.Stdout = &logs
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		default:
			_ = command.Process.Kill()
			<-done
		}
	})
	client := shutdownTestClient()
	ready := false
	readyCtx, readyCancel := context.WithTimeout(ctx, readinessTimeout)
	defer readyCancel()
	retry := time.NewTicker(readinessInterval)
	defer retry.Stop()
	for !ready {
		select {
		case err := <-done:
			done <- err
			t.Fatalf("serve exited before listening: %v\n%s", err, logs.String())
		case <-readyCtx.Done():
			t.Fatal("serve did not become ready")
		case <-retry.C:
			request, err := http.NewRequestWithContext(readyCtx, http.MethodGet, "http://"+addr+"/healthz", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				ready = response.StatusCode == http.StatusOK
			}
		}
	}
	for _, path := range []string{"/metrics", "/openapi.json", "/"} {
		response, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("GET %s: status %d, CSP %q", path, response.StatusCode, response.Header.Get("Content-Security-Policy"))
		}
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		done <- err
		if err != nil {
			t.Fatalf("SIGTERM exit: %v\n%s", err, logs.String())
		}
	case <-time.After(shutdownGrace + shutdownAllowance):
		t.Fatal("serve did not stop after SIGTERM")
	}
}

// TestShutdownClientAllowsSlowResponse covers responses delayed by system load.
func TestShutdownClientAllowsSlowResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	response, err := shutdownTestClient().Get(server.URL)
	if err != nil {
		t.Fatalf("delayed response: %v", err)
	}
	response.Body.Close()
}

func shutdownTestClient() *http.Client { return &http.Client{Timeout: requestTimeout} }
