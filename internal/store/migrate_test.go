package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationDatabase(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("EARNEST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("EARNEST_TEST_DATABASE_URL is not set")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.Database = "postgres"
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect to test administrator")
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	name := "earnest_migrations_" + hex.EncodeToString(id[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0"); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test pool DSN")
	}
	poolConfig.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	db := &Store{pool: pool}
	t.Cleanup(db.Close)
	return db
}

func TestMigrationFailureAndChecksum(t *testing.T) {
	db := migrationDatabase(t)
	ctx := context.Background()
	files := fstest.MapFS{
		"migrations/0001_first.sql": {Data: []byte("CREATE TABLE first_table (id bigint)")},
		"migrations/0002_fail.sql":  {Data: []byte("CREATE TABLE rolled_back (id bigint); SELECT nonexistent_function()")},
	}
	if err := db.migrate(ctx, files); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var exists bool
	if err := db.pool.QueryRow(ctx, "SELECT to_regclass('rolled_back') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("failed migration not atomic: %v %v", exists, err)
	}
	var count int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed migration recorded: %d %v", count, err)
	}
	files["migrations/0002_fail.sql"].Data = []byte("CREATE TABLE rolled_back (id bigint)")
	if err := db.migrate(ctx, files); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	files["migrations/0001_first.sql"].Data = []byte("CREATE TABLE first_table (id text)")
	if err := db.migrate(ctx, files); err == nil {
		t.Fatal("changed applied migration accepted")
	}
}

func TestRejectUnknownSchemaBeforeApplying(t *testing.T) {
	db := migrationDatabase(t)
	ctx := context.Background()
	files := fstest.MapFS{"migrations/0001_first.sql": {Data: []byte("CREATE TABLE first_table(id bigint)")}}
	if err := db.migrate(ctx, files); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO schema_migrations(version,name,checksum) VALUES(99,'future',decode(repeat('ab',32),'hex'))"); err != nil {
		t.Fatal(err)
	}
	files["migrations/0002_new.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE should_not_exist(id bigint)")}
	if err := db.migrate(ctx, files); err == nil {
		t.Fatal("future schema accepted")
	}
	var exists bool
	if err := db.pool.QueryRow(ctx, "SELECT to_regclass('should_not_exist') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("modified unsupported database: %v %v", exists, err)
	}
}

func TestRejectOutOfOrderMigrationBeforeApplying(t *testing.T) {
	db := migrationDatabase(t)
	ctx := t.Context()
	files := fstest.MapFS{
		"migrations/0001_first.sql": {Data: []byte("CREATE TABLE first_table(id bigint)")},
		"migrations/0004_later.sql": {Data: []byte("CREATE TABLE later_table(id bigint)")},
	}
	if err := db.migrate(ctx, files); err != nil {
		t.Fatal(err)
	}
	files["migrations/0002_inserted.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE should_not_exist(id bigint)")}
	files["migrations/0005_latest.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE latest_table(id bigint)")}
	if err := db.migrate(ctx, files); err == nil {
		t.Error("migration predating an applied version was accepted")
	}
	var mutated bool
	if err := db.pool.QueryRow(ctx, "SELECT to_regclass('should_not_exist') IS NOT NULL OR to_regclass('latest_table') IS NOT NULL").Scan(&mutated); err != nil || mutated {
		t.Fatalf("out-of-order migration set changed the database: %v %v", mutated, err)
	}
}

func TestMigrationBindsOnlyLegacyRuleJobs(t *testing.T) {
	db := migrationDatabase(t)
	ctx := t.Context()
	// Apply the real schema immediately before the generation backfill.
	before := fstest.MapFS{}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "migrations/0009_" {
			continue
		}
		data, err := fs.ReadFile(migrations, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: data}
	}
	if err := db.migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO jobs(kind,payload,unique_key) VALUES
	 ('rule','{"entry_ids":[42]}','legacy'),
	 ('rule','{"generation":"stale","entry_ids":[42]}','stale'),
	 ('rule','{"generation":"","entry_ids":[42]}','empty'),
	 ('edition','{"edition_id":42}','other')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var legacy, stale, empty string
	var otherHasGeneration bool
	if err := db.pool.QueryRow(ctx, `SELECT
	 (SELECT payload->>'generation' FROM jobs WHERE unique_key='legacy'),
	 (SELECT payload->>'generation' FROM jobs WHERE unique_key='stale'),
	 (SELECT payload->>'generation' FROM jobs WHERE unique_key='empty'),
	 (SELECT payload ? 'generation' FROM jobs WHERE unique_key='other')`).Scan(&legacy, &stale, &empty, &otherHasGeneration); err != nil {
		t.Fatal(err)
	}
	current, err := db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if legacy != current || stale != "stale" || empty != "" || otherHasGeneration {
		t.Fatalf("legacy=%q current=%q stale=%q empty=%q other=%v", legacy, current, stale, empty, otherHasGeneration)
	}
	if _, err := db.RotateGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var replay string
	if err := db.pool.QueryRow(ctx, `SELECT payload->>'generation' FROM jobs WHERE unique_key='legacy'`).Scan(&replay); err != nil {
		t.Fatal(err)
	}
	if replay != legacy {
		t.Fatal("migration rebound a restored job", replay, legacy)
	}
}
