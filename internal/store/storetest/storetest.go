// Package storetest provides isolated PostgreSQL databases for integration tests.
// The test role needs CREATEDB and permission to install the vector extension.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// Template is a migrated, disconnected database that can be cloned quickly.
// Keep its parent test alive until all tests using it have finished.
type Template struct {
	config *pgx.ConnConfig
	name   string
	mu     sync.Mutex
}

// NewTemplate skips when EARNEST_TEST_DATABASE_URL is unset. Its database and
// all clones are removed by test cleanup. Share one across parallel subtests.
func NewTemplate(t testing.TB) *Template {
	t.Helper()
	dsn := os.Getenv("EARNEST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("EARNEST_TEST_DATABASE_URL is not set")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid EARNEST_TEST_DATABASE_URL")
	}
	template := &Template{config: cfg, name: databaseName(t)}
	template.create(t, template.name, "template0")
	template.cleanup(t, template.name)
	db, err := store.Open(context.Background(), template.dsn(template.name))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return template
}

// New returns a pool connected to a fresh clone and closes it before cleanup.
func (template *Template) New(t testing.TB) *store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), template.DatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

// DatabaseURL creates a migrated clone; the caller must close its connections
// before test cleanup. Use this when testing independent pools or subcommands.
func (template *Template) DatabaseURL(t testing.TB) string {
	t.Helper()
	name := databaseName(t)
	// PostgreSQL cannot clone a template while another clone is being copied.
	template.mu.Lock()
	defer template.mu.Unlock()
	template.create(t, name, template.name)
	template.cleanup(t, name)
	return template.dsn(name)
}

// EmptyDatabaseURL creates an empty database for migration tests.
func (template *Template) EmptyDatabaseURL(t testing.TB) string {
	t.Helper()
	name := databaseName(t)
	template.create(t, name, "template0")
	template.cleanup(t, name)
	return template.dsn(name)
}

func databaseName(t testing.TB) string {
	t.Helper()
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	return "earnest_test_" + hex.EncodeToString(id[:])
}

func (template *Template) dsn(name string) string {
	dsn := template.config.ConnString()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, _ := url.Parse(dsn)
		u.Path = "/" + name
		u.RawPath = ""
		query := u.Query()
		query.Del("dbname")
		query.Del("database")
		u.RawQuery = query.Encode()
		return u.String()
	}
	return dsn + " dbname=" + name
}

func (template *Template) admin(t testing.TB) *pgx.Conn {
	t.Helper()
	cfg := template.config.Copy()
	cfg.Database = "postgres"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect to test database administrator")
	}
	return conn
}

func (template *Template) create(t testing.TB, name, source string) {
	t.Helper()
	conn := template.admin(t)
	defer conn.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE "+pgx.Identifier{source}.Sanitize())
	if err != nil {
		t.Fatalf("create isolated test database: %v", err)
	}
}

func (template *Template) cleanup(t testing.TB, name string) {
	t.Helper()
	t.Cleanup(func() {
		conn := template.admin(t)
		defer conn.Close(context.Background())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// FORCE is scoped to the random database this helper owns.
		if _, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated test database: %v", err)
		}
	})
}
