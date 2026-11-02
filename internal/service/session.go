package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const sessionIdle = 24 * time.Hour
const sessionAbsolute = 30 * 24 * time.Hour

// SessionCredentials contains the cookie and its associated CSRF secret.
type SessionCredentials struct {
	ID        string
	CSRF      string
	ExpiresAt time.Time
}

// Session contains metadata for one web session; IDHash is safe to use to identify it.
type Session struct {
	IDHash    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateSession creates a web session for a human principal.
func (a *Auth) CreateSession(ctx context.Context, actor Actor) (SessionCredentials, error) {
	if actor.Kind != PrincipalHuman {
		return SessionCredentials{}, ErrForbidden
	}
	id, err := makeSecret("")
	if err != nil {
		return SessionCredentials{}, err
	}
	csrf := make([]byte, 32)
	if _, err := rand.Read(csrf); err != nil {
		return SessionCredentials{}, fmt.Errorf("generate CSRF secret: %w", err)
	}
	now := a.now()
	expires := now.Add(sessionIdle)
	tag, err := a.db.Pool().Exec(ctx, "INSERT INTO sessions (id_hash,principal_id,csrf_secret,created_at,expires_at) SELECT $1,id,$3,$4,$5 FROM principals WHERE id=$2 AND user_id=$6 AND kind='human'", hashSecret(id), actor.PrincipalID, csrf, now, expires, actor.UserID)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("create session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return SessionCredentials{}, ErrForbidden
	}
	return SessionCredentials{ID: id, CSRF: base64.RawURLEncoding.EncodeToString(csrf), ExpiresAt: expires}, nil
}

// ResolveSession authenticates and refreshes the idle expiry up to the absolute limit.
func (a *Auth) ResolveSession(ctx context.Context, id string) (Actor, error) {
	now := a.now()
	var actor Actor
	err := a.db.Pool().QueryRow(ctx, "UPDATE sessions s SET expires_at=LEAST($2::timestamptz + $3::interval,s.created_at + $4::interval) FROM principals p WHERE s.id_hash=$1 AND s.principal_id=p.id AND p.kind='human' AND s.expires_at>$2 AND s.created_at + $4::interval>$2 RETURNING p.user_id,p.id", hashSecret(id), now, sessionIdle, sessionAbsolute).Scan(&actor.UserID, &actor.PrincipalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrUnauthorized
	}
	if err != nil {
		return Actor{}, fmt.Errorf("resolve session: %w", err)
	}
	actor.Kind = PrincipalHuman
	actor.Via = CredentialSession
	actor.Scopes = append([]Scope(nil), HumanScopes...)
	return actor, nil
}

// VerifyCSRF checks the session-bound CSRF token without extending the session.
func (a *Auth) VerifyCSRF(ctx context.Context, id, token string) error {
	var secret []byte
	err := a.db.Pool().QueryRow(ctx, "SELECT csrf_secret FROM sessions WHERE id_hash=$1 AND expires_at>$2 AND created_at + $3::interval>$2", hashSecret(id), a.now(), sessionAbsolute).Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("verify CSRF: %w", err)
	}
	provided, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || subtle.ConstantTimeCompare(secret, provided) != 1 {
		return ErrForbidden
	}
	return nil
}

// RevokeSession removes a session belonging to the current user.
func (a *Auth) RevokeSession(ctx context.Context, actor Actor, id string) error {
	if actor.Kind != PrincipalHuman {
		return ErrForbidden
	}
	tag, err := a.db.Pool().Exec(ctx, "DELETE FROM sessions s USING principals p WHERE s.id_hash=$1 AND s.principal_id=p.id AND p.user_id=$2", hashSecret(id), actor.UserID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSessions lists session identifiers as hashes and their expiry times.
func (a *Auth) ListSessions(ctx context.Context, actor Actor) ([]Session, error) {
	if actor.Kind != PrincipalHuman {
		return nil, ErrForbidden
	}
	rows, err := a.db.Pool().Query(ctx, "SELECT s.id_hash,s.created_at,s.expires_at FROM sessions s JOIN principals p ON p.id=s.principal_id WHERE p.user_id=$1 ORDER BY s.created_at", actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var result []Session
	for rows.Next() {
		var s Session
		var hash []byte
		if err := rows.Scan(&hash, &s.CreatedAt, &s.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		s.IDHash = base64.RawURLEncoding.EncodeToString(hash)
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return result, nil
}

// RevokeListedSession revokes a session using the identifier returned by ListSessions.
func (a *Auth) RevokeListedSession(ctx context.Context, actor Actor, idHash string) error {
	if actor.Kind != PrincipalHuman {
		return ErrForbidden
	}
	hash, err := base64.RawURLEncoding.DecodeString(idHash)
	if err != nil || len(hash) != 32 {
		return ErrInvalid
	}
	tag, err := a.db.Pool().Exec(ctx, "DELETE FROM sessions s USING principals p WHERE s.id_hash=$1 AND s.principal_id=p.id AND p.user_id=$2", hash, actor.UserID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
