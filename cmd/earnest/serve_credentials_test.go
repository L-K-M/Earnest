package main

import (
	"bytes"
	"context"
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

func TestServeStartsWithBlockedProviderKey(t *testing.T) {
	const apiKey = "sk-must-not-be-logged"
	dsn := storetest.NewTemplate(t).DatabaseURL(t)
	binary := shutdownBinary(t)
	var listeners [2]net.Listener
	var addrs [2]string
	for i := range listeners {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners[i], addrs[i] = listener, listener.Addr().String()
	}
	for _, listener := range listeners {
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), readinessTimeout+shutdownGrace+shutdownAllowance)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "serve")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "EARNEST_") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env,
		"EARNEST_DATABASE_URL="+dsn,
		"EARNEST_SECRET_KEY="+strings.Repeat("x", 32),
		"EARNEST_HTTP_ADDR="+addrs[0],
		"EARNEST_CONTENT_ADDR="+addrs[1],
		"EARNEST_BASE_URL=http://"+addrs[0],
		"EARNEST_PUSH_VAPID_KEY_FILE="+filepath.Join(t.TempDir(), "vapid.json"),
		"EARNEST_LLM_PROVIDER_LOCAL_KIND=openai",
		"EARNEST_LLM_PROVIDER_LOCAL_BASE_URL=http://models.invalid:11434/v1",
		"EARNEST_LLM_PROVIDER_LOCAL_API_KEY="+apiKey,
	)
	var logs bytes.Buffer
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	client := shutdownTestClient()
	retry := time.NewTicker(readinessInterval)
	defer retry.Stop()
	for ready := false; !ready; {
		select {
		case err := <-done:
			done <- err
			t.Fatalf("serve exited before listening: %v\n%s", err, logs.String())
		case <-ctx.Done():
			t.Fatal("serve did not become ready")
		case <-retry.C:
			if response, err := client.Get("http://" + addrs[0] + "/healthz"); err == nil {
				_ = response.Body.Close()
				ready = response.StatusCode == http.StatusOK
			}
		}
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := <-done
	done <- err
	if err != nil {
		t.Fatalf("SIGTERM exit: %v\n%s", err, logs.String())
	}
	if !strings.Contains(logs.String(), "LLM provider disabled") || strings.Contains(logs.String(), apiKey) {
		t.Fatalf("startup must explain the disabled provider without its key:\n%s", logs.String())
	}
}
