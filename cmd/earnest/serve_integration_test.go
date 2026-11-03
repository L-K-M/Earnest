package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestServeShutdown(t *testing.T) {
	template := storetest.NewTemplate(t)
	dsn := template.DatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "earnest")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-p=2")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
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
	client := &http.Client{Timeout: time.Second}
	ready := false
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for !ready {
		select {
		case err := <-done:
			done <- err
			t.Fatalf("serve exited before listening: %v\n%s", err, logs.String())
		case <-deadline.C:
			t.Fatal("serve did not become ready")
		case <-retry.C:
			response, err := client.Get("http://" + addr + "/healthz")
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
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop after SIGTERM")
	}
}
