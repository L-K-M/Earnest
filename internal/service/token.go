package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

type TokenKind string

const (
	TokenApp    TokenKind = "app"
	TokenAgent  TokenKind = "agent"
	TokenCompat TokenKind = "compat"
)

type Audience string

const (
	AudienceAPI Audience = "api"
	AudienceMCP Audience = "mcp"
)

type TokenOptions struct {
	Kind        TokenKind
	Name        string
	Scopes      []Scope
	Audience    Audience
	DailyBudget int
	ExpiresAt   *time.Time
}
type Token struct {
	ID          int64
	PrincipalID int64
	Name        string
	Kind        TokenKind
	Audience    Audience
	Scopes      []Scope
	DailyBudget int
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}
type TokenSecret struct {
	Token  Token
	Secret string
}

func makeSecret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate credential: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}
func hashSecret(s string) []byte  { h := sha256.Sum256([]byte(s)); return h[:] }
func humanAdmin(actor Actor) bool { return actor.Kind == PrincipalHuman && actor.Has(ScopeTokensWrite) }
func scopeStrings(scopes []Scope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}

// CreateToken issues a secret once and stores only its hash.
func (a *Auth) CreateToken(ctx context.Context, actor Actor, opts TokenOptions) (TokenSecret, error) {
	if !humanAdmin(actor) {
		return TokenSecret{}, ErrForbidden
	}
	if opts.Kind != TokenApp && opts.Kind != TokenAgent {
		return TokenSecret{}, fmt.Errorf("token kind: %w", ErrInvalid)
	}
	if strings.TrimSpace(opts.Name) == "" || (opts.Audience != AudienceAPI && opts.Audience != AudienceMCP) || opts.DailyBudget < 0 || (opts.ExpiresAt != nil && !opts.ExpiresAt.After(a.now())) {
		return TokenSecret{}, fmt.Errorf("token options: %w", ErrInvalid)
	}
	if opts.Kind == TokenApp {
		if opts.Audience != AudienceAPI {
			return TokenSecret{}, fmt.Errorf("app audience: %w", ErrInvalid)
		}
		opts.Scopes = HumanScopes
	} else {
		for _, s := range opts.Scopes {
			if !slices.Contains(AgentScopes, s) {
				return TokenSecret{}, fmt.Errorf("agent scope: %w", ErrInvalid)
			}
		}
	}
	prefix := "earnest_app_"
	if opts.Kind == TokenAgent {
		prefix = "earnest_agent_"
	}
	secret, err := makeSecret(prefix)
	if err != nil {
		return TokenSecret{}, err
	}
	tok := Token{PrincipalID: actor.PrincipalID, Name: opts.Name, Kind: opts.Kind, Audience: opts.Audience, Scopes: append([]Scope(nil), opts.Scopes...), DailyBudget: opts.DailyBudget, ExpiresAt: opts.ExpiresAt, CreatedAt: a.now()}
	err = a.db.WithTx(ctx, func(tx pgx.Tx) error {
		var valid bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM principals WHERE id=$1 AND user_id=$2 AND kind='human')", actor.PrincipalID, actor.UserID).Scan(&valid); err != nil {
			return fmt.Errorf("check principal: %w", err)
		}
		if !valid {
			return ErrForbidden
		}
		if opts.Kind == TokenAgent {
			if err := tx.QueryRow(ctx, "INSERT INTO principals (user_id,kind,name,created_at) VALUES ($1,'agent',$2,$3) RETURNING id", actor.UserID, opts.Name, tok.CreatedAt).Scan(&tok.PrincipalID); err != nil {
				return fmt.Errorf("create agent principal: %w", err)
			}
		}
		if err := tx.QueryRow(ctx, "INSERT INTO tokens (principal_id,name,token_hash,kind,audience,scopes,daily_budget,expires_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id", tok.PrincipalID, tok.Name, hashSecret(secret), tok.Kind, tok.Audience, scopeStrings(tok.Scopes), tok.DailyBudget, tok.ExpiresAt, tok.CreatedAt).Scan(&tok.ID); err != nil {
			return fmt.Errorf("create token: %w", err)
		}
		return nil
	})
	if err != nil {
		return TokenSecret{}, err
	}
	return TokenSecret{Token: tok, Secret: secret}, nil
}

// ListTokens returns credential metadata for the user's tokens.
func (a *Auth) ListTokens(ctx context.Context, actor Actor) ([]Token, error) {
	if !humanAdmin(actor) {
		return nil, ErrForbidden
	}
	rows, err := a.db.Pool().Query(ctx, "SELECT t.id,t.principal_id,t.name,t.kind,t.audience,t.scopes,t.daily_budget,t.expires_at,t.last_used_at,t.revoked_at,t.created_at FROM tokens t JOIN principals p ON p.id=t.principal_id WHERE p.user_id=$1 ORDER BY t.id", actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()
	var result []Token
	for rows.Next() {
		var t Token
		var scopes []string
		if err := rows.Scan(&t.ID, &t.PrincipalID, &t.Name, &t.Kind, &t.Audience, &scopes, &t.DailyBudget, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		for _, s := range scopes {
			t.Scopes = append(t.Scopes, Scope(s))
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return result, nil
}

// RevokeToken revokes a user's token and publishes a change atomically.
func (a *Auth) RevokeToken(ctx context.Context, actor Actor, id int64) error {
	if !humanAdmin(actor) {
		return ErrForbidden
	}
	err := a.db.WithTx(ctx, func(tx pgx.Tx) error {
		alloc, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{actor.UserID: 1})
		if err != nil {
			return err
		}
		rev := alloc.Revs[actor.UserID].First
		tag, err := tx.Exec(ctx, "UPDATE tokens t SET revoked_at=$3,rev=$4 FROM principals p WHERE t.id=$1 AND t.principal_id=p.id AND p.user_id=$2 AND t.revoked_at IS NULL", id, actor.UserID, a.now(), rev)
		if err != nil {
			return fmt.Errorf("revoke token: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		payload, _ := json.Marshal(map[string]int64{"id": id})
		if _, err := tx.Exec(ctx, "INSERT INTO changes (user_id,rev,type,payload,created_at) VALUES ($1,$2,'token.revoked',$3,$4)", actor.UserID, rev, payload, a.now()); err != nil {
			return fmt.Errorf("log token revocation: %w", err)
		}
		return nil
	})
	if err == nil {
		a.events.publish(actor.UserID)
	}
	return err
}

// RevokeTokenByID supports trusted server-side administration.
func (a *Auth) RevokeTokenByID(ctx context.Context, id int64) error {
	var username string
	err := a.db.Pool().QueryRow(ctx, "SELECT u.username FROM tokens t JOIN principals p ON p.id=t.principal_id JOIN users u ON u.id=p.user_id WHERE t.id=$1", id).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find token owner: %w", err)
	}
	actor, err := a.UserActor(ctx, username)
	if err != nil {
		return err
	}
	return a.RevokeToken(ctx, actor, id)
}

// ResolveToken authenticates a token for the specified audience.
func (a *Auth) ResolveToken(ctx context.Context, secret string, audience Audience) (Actor, error) {
	return a.resolveToken(ctx, secret, audience, false)
}
func (a *Auth) resolveToken(ctx context.Context, secret string, audience Audience, compat bool) (Actor, error) {
	hash := hashSecret(secret)
	var actor Actor
	var stored []byte
	var kind TokenKind
	var aud Audience
	var scopes []string
	var expires, revoked *time.Time
	err := a.db.Pool().QueryRow(ctx, "SELECT t.id,p.user_id,p.id,p.kind,t.kind,t.audience,t.scopes,t.expires_at,t.revoked_at,t.token_hash FROM tokens t JOIN principals p ON p.id=t.principal_id WHERE t.token_hash=$1", hash).Scan(&actor.TokenID, &actor.UserID, &actor.PrincipalID, &actor.Kind, &kind, &aud, &scopes, &expires, &revoked, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrUnauthorized
	}
	if err != nil {
		return Actor{}, fmt.Errorf("resolve token: %w", err)
	}
	if subtle.ConstantTimeCompare(hash, stored) != 1 || aud != audience || revoked != nil || (expires != nil && !a.now().Before(*expires)) || (kind == TokenCompat) != compat {
		return Actor{}, ErrUnauthorized
	}
	switch kind {
	case TokenApp:
		actor.Via = CredentialAppToken
		actor.Scopes = append([]Scope(nil), HumanScopes...)
	case TokenAgent:
		actor.Via = CredentialAgentToken
		for _, s := range scopes {
			if !slices.Contains(AgentScopes, Scope(s)) {
				return Actor{}, ErrUnauthorized
			}
			actor.Scopes = append(actor.Scopes, Scope(s))
		}
	case TokenCompat:
		actor.Via = CredentialCompat
		actor.Scopes = append([]Scope(nil), CompatScopes...)
	default:
		return Actor{}, ErrUnauthorized
	}
	_, err = a.db.Pool().Exec(ctx, "UPDATE tokens SET last_used_at=$2 WHERE id=$1 AND (last_used_at IS NULL OR last_used_at <= $2::timestamptz - interval '1 minute')", actor.TokenID, a.now())
	if err != nil {
		return Actor{}, fmt.Errorf("update token use: %w", err)
	}
	return actor, nil
}
