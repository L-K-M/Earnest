package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FeedProber discovers, fetches and sanitises a feed outside a database transaction.
// Probe errors may implement interface{ Class() string } for feed-health classification.
type FeedProber interface {
	Probe(context.Context, string, *FeedCredentials) (string, FeedMeta, []IngestItem, error)
}

// ProbeError classifies a discovery failure using the fetch error vocabulary.
type ProbeError interface {
	error
	Class() string
}

// Deps supplies the clock, outbound probe and deployment secret (at least 32 bytes).
type Deps struct {
	Clock        func() time.Time
	FeedProber   FeedProber
	SecretKey    []byte
	PushRegistry *push.Registry
	Outbound     *outbound.Client
}

// Service owns domain operations. Its dependencies must be safe for concurrent use.
type Service struct {
	store        *store.Store
	now          func() time.Time
	prober       FeedProber
	credentials  cipher.AEAD
	events       broker
	pushRegistry *push.Registry
	outbound     *outbound.Client
}

// New derives a purpose-separated credential key without retaining the deployment secret.
func New(db *store.Store, deps Deps) (*Service, error) {
	if db == nil || len(deps.SecretKey) < 32 {
		return nil, fmt.Errorf("store and 32-byte secret required: %w", ErrInvalid)
	}
	key, err := hkdf.Key(sha256.New, deps.SecretKey, nil, "earnest/feed-credentials/v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive credential key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("credential envelope: %w", err)
	}
	now := deps.Clock
	if now == nil {
		now = time.Now
	}
	return &Service{store: db, now: now, prober: deps.FeedProber, credentials: aead, pushRegistry: deps.PushRegistry, outbound: deps.Outbound}, nil
}

func (s *Service) checkActor(ctx context.Context, a Actor, scope Scope) error {
	return checkActorQuery(ctx, s.store.Pool(), a, scope)
}

func checkActorQuery(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, a Actor, scope Scope) error {
	if a.UserID <= 0 || a.PrincipalID <= 0 {
		return ErrUnauthorized
	}
	if !a.Has(scope) || (scope == ScopeFeedsWrite && a.Kind != PrincipalHuman) {
		return ErrForbidden
	}
	var ok bool
	err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM principals WHERE id=$1 AND user_id=$2 AND kind=$3)", a.PrincipalID, a.UserID, string(a.Kind)).Scan(&ok)
	if err != nil {
		return fmt.Errorf("authorize principal: %w", err)
	}
	if !ok {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) sealCredentials(c *FeedCredentials) ([]byte, error) {
	if c == nil {
		return nil, nil
	}
	body, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("encode credentials: %w", err)
	}
	nonce := make([]byte, s.credentials.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("credential nonce: %w", err)
	}
	return s.credentials.Seal(nonce, nonce, body, []byte("earnest/feed-credentials/v1")), nil
}
func (s *Service) openCredentials(body []byte) (*FeedCredentials, error) {
	if len(body) == 0 {
		return nil, nil
	}
	n := s.credentials.NonceSize()
	if len(body) < n {
		return nil, fmt.Errorf("credential envelope: %w", ErrInvalid)
	}
	plain, err := s.credentials.Open(nil, body[:n], body[n:], []byte("earnest/feed-credentials/v1"))
	if err != nil {
		return nil, fmt.Errorf("decrypt feed credentials: %w", ErrUnavailable)
	}
	var c FeedCredentials
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}
	return &c, nil
}

// change allocates only an event that will commit. The transaction already holds
// the global counter lock, so discovering exact user/event counts cannot race.
func (s *Service) change(ctx context.Context, tx pgx.Tx, user int64, kind syncmodel.ChangeType, payload any) (int64, error) {
	allocation, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{user: 1})
	if err != nil {
		return 0, err
	}
	rev := allocation.Revs[user].First
	if err := insertEvent(ctx, tx, user, rev, string(kind), payload, s.now().UTC()); err != nil {
		return 0, err
	}
	return rev, nil
}

func domainError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return fmt.Errorf("duplicate resource: %w", ErrConflict)
		case "23503", "23514", "22001", "22003":
			return fmt.Errorf("invalid resource: %w", ErrInvalid)
		}
	}
	return err
}
func validName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 1024 }
