package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrations embed.FS

const migrationLockID int64 = 0x4561726e657374

// Migrate applies embedded, numbered migrations once, under a session lock.
// Existing migration checksums are verified; applied files must never change.
func (s *Store) Migrate(ctx context.Context) error {
	return s.migrate(ctx, migrations)
}

func (s *Store) migrate(ctx context.Context, files fs.FS) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		// A cancelled lock request can race with acquisition. Discard the session.
		_ = conn.Conn().Close(context.Background())
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
 version bigint PRIMARY KEY CHECK (version > 0), name text NOT NULL,
 checksum bytea NOT NULL CHECK (octet_length(checksum) = 32), applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	type migration struct {
		version  int64
		name     string
		body     []byte
		checksum [32]byte
	}
	pending := make([]migration, 0, len(names))
	known := make(map[int64]migration, len(names))
	var previous int64
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.ParseInt(strings.SplitN(base, "_", 2)[0], 10, 64)
		if err != nil || version <= previous {
			return fmt.Errorf("invalid migration name %q", name)
		}
		previous = version
		body, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		file := migration{version: version, name: base, body: body, checksum: sha256.Sum256(body)}
		pending = append(pending, file)
		known[version] = file
	}
	// Validate the whole ledger before executing any pending SQL. An older binary
	// must not alter a database written by a newer or divergent migration set.
	rows, err := conn.Query(ctx, "SELECT version,name,checksum FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		var name string
		var checksum []byte
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration ledger: %w", err)
		}
		file, ok := known[version]
		if !ok {
			rows.Close()
			return fmt.Errorf("database migration %d is unsupported by this binary", version)
		}
		if file.name != name || string(file.checksum[:]) != string(checksum) {
			rows.Close()
			return fmt.Errorf("migration %s differs from applied version", file.name)
		}
		applied[version] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	for _, file := range pending {
		if applied[file.version] {
			continue
		}
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(file.body)); err != nil {
				return fmt.Errorf("execute migration %s: %w", file.name, err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version,name,checksum) VALUES ($1,$2,$3)", file.version, file.name, file.checksum[:]); err != nil {
				return fmt.Errorf("record migration %s: %w", file.name, err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
