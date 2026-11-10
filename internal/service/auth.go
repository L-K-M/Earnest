package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 1
	passwordSaltSize    = 16
	passwordHashSize    = 32
	loginLimit          = 5
	loginWindow         = 15 * time.Minute
)

type loginAttempts struct {
	count int
	until time.Time
}

type loginOutcome uint8

const (
	loginAborted loginOutcome = iota
	loginFailed
	loginSucceeded
)

// Auth manages identity and credentials independently of the reader service.
type Auth struct {
	db        *store.Store
	now       func() time.Time
	secretKey []byte
	events    broker
	mu        sync.Mutex
	attempts  map[string]loginAttempts
	// In-flight reservations are separate from expiring failure counters:
	// a slow password check must retain its slot across a window boundary.
	pendingLogins map[string]int
}

// NewAuth constructs an identity service. clock may be nil for the system clock.
func NewAuth(db *store.Store, clock func() time.Time, secretKey []byte) *Auth {
	if clock == nil {
		clock = time.Now
	}
	return &Auth{db: db, now: clock, secretKey: append([]byte(nil), secretKey...), attempts: make(map[string]loginAttempts), pendingLogins: make(map[string]int)}
}

func passwordHash(password string) (string, error) {
	salt := make([]byte, passwordSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordParallelism, passwordHashSize)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemory, passwordIterations, passwordParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func checkPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", passwordMemory, passwordIterations, passwordParallelism) {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[4])
	hash, e2 := base64.RawStdEncoding.DecodeString(parts[5])
	if e1 != nil || e2 != nil || len(salt) != passwordSaltSize || len(hash) != passwordHashSize {
		return false
	}
	candidate := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordParallelism, passwordHashSize)
	return subtle.ConstantTimeCompare(candidate, hash) == 1
}

// CreateUser creates a user and its human principal atomically.
func (a *Auth) CreateUser(ctx context.Context, username, password string) (Actor, error) {
	if strings.TrimSpace(username) == "" || password == "" {
		return Actor{}, fmt.Errorf("username and password required: %w", ErrInvalid)
	}
	hash, err := passwordHash(password)
	if err != nil {
		return Actor{}, err
	}
	var actor Actor
	err = a.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "INSERT INTO users (username,password_hash) VALUES ($1,$2) RETURNING id", username, hash).Scan(&actor.UserID); err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				return ErrConflict
			}
			return fmt.Errorf("create user: %w", err)
		}
		if err := tx.QueryRow(ctx, "INSERT INTO principals (user_id,kind,name) VALUES ($1,'human',$2) RETURNING id", actor.UserID, username).Scan(&actor.PrincipalID); err != nil {
			return fmt.Errorf("create human principal: %w", err)
		}
		return nil
	})
	if err != nil {
		return Actor{}, err
	}
	actor.Kind = PrincipalHuman
	actor.Scopes = append([]Scope(nil), HumanScopes...)
	return actor, nil
}

// SetPassword changes a user's password.
func (a *Auth) SetPassword(ctx context.Context, username, password string) error {
	if password == "" {
		return fmt.Errorf("password required: %w", ErrInvalid)
	}
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	tag, err := a.db.Pool().Exec(ctx, "UPDATE users SET password_hash=$2 WHERE username=$1", username, hash)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserActor returns the human principal for server-side administration.
func (a *Auth) UserActor(ctx context.Context, username string) (Actor, error) {
	var actor Actor
	err := a.db.Pool().QueryRow(ctx, "SELECT u.id,p.id FROM users u JOIN principals p ON p.user_id=u.id AND p.kind='human' WHERE u.username=$1", username).Scan(&actor.UserID, &actor.PrincipalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrNotFound
	}
	if err != nil {
		return Actor{}, fmt.Errorf("find user: %w", err)
	}
	actor.Kind = PrincipalHuman
	actor.Scopes = append([]Scope(nil), HumanScopes...)
	return actor, nil
}

// failureKeys returns the throttle keys for a username/client pair.
func failureKeys(username, client string) []string {
	return []string{"u:" + username, "c:" + client}
}

// pruneExpired drops entries whose lockout window has elapsed.
// Callers must hold a.mu.
func (a *Auth) pruneExpired(now time.Time) {
	for key, v := range a.attempts {
		if !now.Before(v.until) {
			delete(a.attempts, key)
		}
	}
}

// startLogin reserves an attempt before database I/O and password hashing.
// Failures plus in-flight checks share the limit for both username and client.
// Every accepted reservation must be completed once with finishLogin.
// Counters are local to this Auth instance and do not survive a restart.
func (a *Auth) startLogin(username, client string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneExpired(a.now())
	keys := failureKeys(username, client)
	for _, key := range keys {
		if a.attempts[key].count+a.pendingLogins[key] >= loginLimit {
			return false
		}
	}
	for _, key := range keys {
		a.pendingLogins[key]++
	}
	return true
}

// finishLogin releases the reservation and records its outcome atomically.
// Success forgives account failures, but never the client's spray counter.
// Database errors release their slot without counting as a wrong password.
func (a *Auth) finishLogin(username, client string, outcome loginOutcome) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	a.pruneExpired(now)
	for _, key := range failureKeys(username, client) {
		if a.pendingLogins[key] == 1 {
			delete(a.pendingLogins, key)
		} else {
			a.pendingLogins[key]--
		}
		if outcome == loginFailed {
			v := a.attempts[key]
			if v.until.IsZero() {
				v.until = now.Add(loginWindow)
			}
			v.count++
			a.attempts[key] = v
		}
	}
	if outcome == loginSucceeded {
		delete(a.attempts, "u:"+username)
	}
}

// Authenticate checks a password and rate limits attempts by username and client key.
func (a *Auth) Authenticate(ctx context.Context, username, password, clientKey string) (Actor, error) {
	if !a.startLogin(username, clientKey) {
		return Actor{}, ErrRateLimited
	}
	outcome := loginAborted
	defer func() { a.finishLogin(username, clientKey, outcome) }()
	var actor Actor
	var hash string
	err := a.db.Pool().QueryRow(ctx, "SELECT u.id,p.id,u.password_hash FROM users u JOIN principals p ON p.user_id=u.id AND p.kind='human' WHERE u.username=$1", username).Scan(&actor.UserID, &actor.PrincipalID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, fmt.Errorf("authenticate: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		hash = "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}
	ok := checkPassword(hash, password)
	if !ok || err != nil {
		outcome = loginFailed
		return Actor{}, ErrUnauthorized
	}
	outcome = loginSucceeded
	actor.Kind = PrincipalHuman
	actor.Scopes = append([]Scope(nil), HumanScopes...)
	return actor, nil
}
