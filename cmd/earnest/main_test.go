package main

import (
	"context"
	"encoding/json"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
