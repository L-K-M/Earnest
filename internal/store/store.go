// Package store owns PostgreSQL persistence and transactional ordering.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const poolMaxConns = 10

// Store owns the connection pool. Close it after all users have stopped.
type Store struct{ pool *pgxpool.Pool }

// Open validates the DSN and verifies connectivity. Migrate is explicit.
// Pool sizing uses ten connections to keep NAS memory use bounded.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, &connectionError{message: "parse database configuration", cause: err}
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = 10 * time.Second
	}
	// A NAS does not need pgx's CPU-scaled default pool size.
	cfg.MaxConns = poolMaxConns
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = time.Hour
	cfg.HealthCheckPeriod = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, &connectionError{message: "create database pool", cause: err}
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, &connectionError{message: "connect to database", cause: err}
	}
	return &Store{pool: pool}, nil
}

// Close releases all pooled connections.
func (s *Store) Close() { s.pool.Close() }

// Pool exposes pgx for service-owned queries; handlers must use the service.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// WithTx commits only if fn succeeds; errors and panics roll back.
func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	if err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, fn); err != nil {
		return fmt.Errorf("transaction: %w", err)
	}
	return nil
}

// connectionError hides driver messages that may contain a DSN or password,
// while retaining the cause for errors.Is/errors.As.
type connectionError struct {
	message string
	cause   error
}

func (e *connectionError) Error() string { return e.message + ": connection settings redacted" }
func (e *connectionError) Unwrap() error { return e.cause }
