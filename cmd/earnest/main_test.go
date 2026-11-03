package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/L-K-M/Earnest/internal/config"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHealthzHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthzHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", body["status"])
	}
	if body["version"] == "" {
		t.Fatal("version field is empty — ldflags defaults should apply")
	}
}

func TestMigrateRequiresDatabase(t *testing.T) {
	t.Setenv("EARNEST_DATABASE_URL", "")
	if err := migrate(); err == nil || !strings.Contains(err.Error(), "EARNEST_DATABASE_URL") {
		t.Fatalf("missing database error: %v", err)
	}
}

func TestMigrate(t *testing.T) {
	template := storetest.NewTemplate(t)
	dsn := template.EmptyDatabaseURL(t)
	t.Setenv("EARNEST_DATABASE_URL", dsn)
	// The maintenance command must not need web configuration or signing keys.
	t.Setenv("EARNEST_SECRET_KEY", "")
	for range 2 {
		if err := migrate(); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := highestMigrationVersion(t)
	var version int
	if err := db.Pool().QueryRow(context.Background(), "SELECT max(version) FROM schema_migrations").Scan(&version); err != nil || version != want {
		t.Fatalf("schema version %d want %d: %v", version, want, err)
	}
}

// highestMigrationVersion returns the highest version number among the SQL
// migration files, so the test follows the migration set instead of a
// hard-coded version.
func highestMigrationVersion(t *testing.T) int {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("..", "..", "internal", "store", "migrations", "*.sql"))
	if err != nil || len(names) == 0 {
		t.Fatalf("glob migrations: %d files, error=%v", len(names), err)
	}
	highest := 0
	for _, name := range names {
		base := strings.TrimSuffix(filepath.Base(name), ".sql")
		prefix, _, ok := strings.Cut(base, "_")
		if !ok {
			t.Fatalf("migration %q has no version prefix", name)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("migration %q has invalid version prefix: %v", name, err)
		}
		if version > highest {
			highest = version
		}
	}
	return highest
}

func TestPushRegistryAndVAPIDPersistence(t *testing.T) {
	cfg := config.Config{PushVAPIDKeyFile: filepath.Join(t.TempDir(), "push", "vapid.json")}
	client, err := outbound.New(outbound.Config{})
	if err != nil {
		t.Fatal(err)
	}
	registry, public, err := buildPushRegistry(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(registry.Kinds()); got != "[ntfy unifiedpush webpush]" || public == "" {
		t.Fatal(got, public)
	}
	info, err := os.Stat(cfg.PushVAPIDKeyFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			keys, err := loadVAPIDKeys(cfg)
			if err != nil || keys.PublicKey != public {
				t.Errorf("persisted public key=%s err=%v", keys.PublicKey, err)
			}
		})
	}
	wg.Wait()
	keys, err := loadVAPIDKeys(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.PushVAPIDKeyFile = ""
	cfg.PushVAPIDPrivateKey = keys.PrivateKey
	cfg.PushVAPIDPublicKey = keys.PublicKey
	cfg.APNsPrivateKey = keys.PrivateKey
	cfg.APNsKeyID = "KEYID"
	cfg.APNsTeamID = "TEAMID"
	registry, configured, err := buildPushRegistry(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(registry.Kinds()); got != "[apns ntfy unifiedpush webpush]" || configured != public {
		t.Fatal(got, configured)
	}
	cfg.PushVAPIDPublicKey = "invalid"
	if _, _, err := buildPushRegistry(cfg, client); err == nil {
		t.Fatal("invalid VAPID pair accepted")
	}
}

func TestVAPIDConcurrentCreationAndCorruption(t *testing.T) {
	cfg := config.Config{PushVAPIDKeyFile: filepath.Join(t.TempDir(), "vapid.json")}
	results := make(chan push.VAPIDKeys, 6)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			keys, err := loadVAPIDKeys(cfg)
			if err != nil {
				t.Error(err)
				return
			}
			results <- keys
		})
	}
	wg.Wait()
	close(results)
	var first push.VAPIDKeys
	for keys := range results {
		if first.PublicKey == "" {
			first = keys
		}
		if !bytes.Equal(first.PrivateKey, keys.PrivateKey) {
			t.Fatal("concurrent creation used different keys")
		}
	}
	if err := os.WriteFile(cfg.PushVAPIDKeyFile, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVAPIDKeys(cfg); err == nil {
		t.Fatal("corrupt key file silently replaced")
	}
	if data, err := os.ReadFile(cfg.PushVAPIDKeyFile); err != nil || string(data) != "corrupt" {
		t.Fatal(string(data), err)
	}
}

func TestDeliveryWorkerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		defer close(done)
		runDelivery(ctx, logger, "test delivery", func(ctx context.Context, limit int) (int, error) {
			if limit != deliveryBatchSize {
				t.Errorf("batch size=%d", limit)
			}
			cancel()
			return 0, errors.New("cancelled send")
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker ignored cancellation")
	}
}
