package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetCompatPassword replaces the compatibility password and returns it once.
func (a *Auth) SetCompatPassword(ctx context.Context, actor Actor) (string, error) {
	if !humanAdmin(actor) {
		return "", ErrForbidden
	}
	secret, err := makeSecret("earnest_compat_")
	if err != nil {
		return "", err
	}
	hash, err := passwordHash(secret)
	if err != nil {
		return "", err
	}
	tag, err := a.db.Pool().Exec(ctx, "UPDATE users SET compat_password_hash=$2 WHERE id=$1 AND EXISTS (SELECT 1 FROM principals WHERE id=$3 AND user_id=$1 AND kind='human')", actor.UserID, hash, actor.PrincipalID)
	if err != nil {
		return "", fmt.Errorf("set compatibility password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrForbidden
	}
	return secret, nil
}

// CompatLogin exchanges the compatibility password for a bearer token.
func (a *Auth) CompatLogin(ctx context.Context, username, password, clientKey string) (TokenSecret, error) {
	if !a.startLogin(username, clientKey) {
		return TokenSecret{}, ErrRateLimited
	}
	outcome := loginAborted
	defer func() { a.finishLogin(username, clientKey, outcome) }()
	var userID, principalID int64
	var hash *string
	err := a.db.Pool().QueryRow(ctx, "SELECT u.id,p.id,u.compat_password_hash FROM users u JOIN principals p ON p.user_id=u.id AND p.kind='human' WHERE u.username=$1", username).Scan(&userID, &principalID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return TokenSecret{}, fmt.Errorf("compatibility login: %w", err)
	}
	candidate := "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if hash != nil {
		candidate = *hash
	}
	if !checkPassword(candidate, password) || err != nil || hash == nil {
		outcome = loginFailed
		return TokenSecret{}, ErrUnauthorized
	}
	outcome = loginSucceeded
	secret, err := makeSecret("earnest_compat_token_")
	if err != nil {
		return TokenSecret{}, err
	}
	tok := Token{PrincipalID: principalID, Name: "Google Reader", Kind: TokenCompat, Audience: AudienceAPI, Scopes: append([]Scope(nil), CompatScopes...), CreatedAt: a.now()}
	err = a.db.Pool().QueryRow(ctx, "INSERT INTO tokens (principal_id,name,token_hash,kind,audience,scopes,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id", principalID, tok.Name, hashSecret(secret), tok.Kind, tok.Audience, scopeStrings(tok.Scopes), tok.CreatedAt).Scan(&tok.ID)
	if err != nil {
		return TokenSecret{}, fmt.Errorf("create compatibility token: %w", err)
	}
	return TokenSecret{Token: tok, Secret: secret}, nil
}

// ResolveCompatToken authenticates a Google Reader bearer token.
func (a *Auth) ResolveCompatToken(ctx context.Context, secret string) (Actor, error) {
	return a.resolveToken(ctx, secret, AudienceAPI, true)
}
