package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// ArticleIntegration names a supported, explicitly configured destination.
type ArticleIntegration string

const (
	IntegrationKarakeep    ArticleIntegration = "karakeep"
	IntegrationHister      ArticleIntegration = "hister"
	integrationSettingsKey                    = "article_integrations"
)

// IntegrationSettings never exposes the saved API token.
type IntegrationSettings struct {
	URL           string `json:"url"`
	DefaultListID string `json:"default_list_id"`
	DefaultLabel  string `json:"default_label"`
	TokenSet      bool   `json:"token_set"`
}

type ArticleIntegrations struct {
	Karakeep IntegrationSettings `json:"karakeep"`
	Hister   IntegrationSettings `json:"hister"`
}

type IntegrationSettingsInput struct {
	URL           string
	DefaultListID string
	DefaultLabel  string
	// Nil preserves a token only when the destination URL is unchanged.
	// An empty string clears it. Disabling the destination clears it too.
	Token *string
	OpID  string
}

type storedIntegration struct {
	URL           string `json:"url"`
	DefaultListID string `json:"default_list_id"`
	DefaultLabel  string `json:"default_label"`
	Token         []byte `json:"token_encrypted,omitempty"`
}

func (p ArticleIntegration) valid() bool { return p == IntegrationKarakeep || p == IntegrationHister }
func (p ArticleIntegration) tokenPurpose(user int64) string {
	return fmt.Sprintf("earnest/article-integration/%s/%d/v1", p, user)
}
func (c storedIntegration) public() IntegrationSettings {
	return IntegrationSettings{URL: c.URL, DefaultListID: c.DefaultListID, DefaultLabel: c.DefaultLabel, TokenSet: len(c.Token) > 0}
}

func (s *Service) GetArticleIntegrations(ctx context.Context, a Actor) (ArticleIntegrations, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return ArticleIntegrations{}, err
	}
	configs, err := s.loadArticleIntegrations(ctx, a.UserID)
	if err != nil {
		return ArticleIntegrations{}, err
	}
	return ArticleIntegrations{Karakeep: configs[IntegrationKarakeep].public(), Hister: configs[IntegrationHister].public()}, nil
}

func decodeArticleIntegrations(raw []byte) (map[ArticleIntegration]storedIntegration, error) {
	configs := map[ArticleIntegration]storedIntegration{}
	if len(raw) == 0 || string(raw) == "null" {
		return configs, nil
	}
	if err := json.Unmarshal(raw, &configs); err != nil {
		return nil, fmt.Errorf("read article integration settings: %w", ErrUnavailable)
	}
	return configs, nil
}

func (s *Service) loadArticleIntegrations(ctx context.Context, user int64) (map[ArticleIntegration]storedIntegration, error) {
	var raw []byte
	if err := s.store.Pool().QueryRow(ctx, "SELECT settings->$2 FROM users WHERE id=$1", user, integrationSettingsKey).Scan(&raw); err != nil {
		return nil, domainError(err)
	}
	return decodeArticleIntegrations(raw)
}

func (s *Service) PutArticleIntegration(ctx context.Context, a Actor, provider ArticleIntegration, in IntegrationSettingsInput) (IntegrationSettings, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return IntegrationSettings{}, err
	}
	if !provider.valid() || !validOpID(in.OpID) {
		return IntegrationSettings{}, ErrInvalid
	}
	server, err := integrationURL(in.URL)
	if err != nil {
		return IntegrationSettings{}, err
	}
	list := strings.TrimSpace(in.DefaultListID)
	if (provider == IntegrationHister && list != "") || (list != "" && !integrationID(list)) {
		return IntegrationSettings{}, fmt.Errorf("default_list_id is supported only for a Karakeep list ID: %w", ErrInvalid)
	}
	label := strings.TrimSpace(in.DefaultLabel)
	if (provider == IntegrationKarakeep && label != "") || len(label) > 256 || strings.IndexFunc(label, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return IntegrationSettings{}, fmt.Errorf("default_label must be a Hister label of at most 256 bytes: %w", ErrInvalid)
	}
	if in.Token != nil && (len(*in.Token) > 4096 || strings.IndexFunc(*in.Token, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0) {
		return IntegrationSettings{}, fmt.Errorf("integration token: %w", ErrInvalid)
	}
	var out IntegrationSettings
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		// Settings updates use the existing short mutation lock. No outbound
		// request runs while this lock is held.
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "PutArticleIntegration/"+string(provider), in.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT settings->$2 FROM users WHERE id=$1 FOR UPDATE", a.UserID, integrationSettingsKey).Scan(&raw); err != nil {
			return domainError(err)
		}
		configs, err := decodeArticleIntegrations(raw)
		if err != nil {
			return err
		}
		previous := configs[provider]
		next := storedIntegration{URL: server, DefaultListID: list, DefaultLabel: label}
		switch {
		case server == "":
			next.DefaultListID = ""
			next.DefaultLabel = ""
		case in.Token != nil && *in.Token != "":
			next.Token, err = s.sealSecret([]byte(*in.Token), provider.tokenPurpose(a.UserID))
			if err != nil {
				return err
			}
		case in.Token == nil && server == previous.URL:
			next.Token = previous.Token
		}
		if server != "" && provider == IntegrationKarakeep && len(next.Token) == 0 {
			return fmt.Errorf("Karakeep requires an API key; enter a new key when changing its URL: %w", ErrInvalid)
		}
		configs[provider] = next
		fragment, err := json.Marshal(map[string]any{integrationSettingsKey: configs})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE users SET settings=settings || $2::jsonb WHERE id=$1", a.UserID, string(fragment)); err != nil {
			return domainError(err)
		}
		out = next.public()
		// Neither the change feed nor the idempotency record gets a secret.
		return store.FinishOp(ctx, tx, a.PrincipalID, in.OpID, out)
	})
	return out, err
}

func integrationURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("integration URL must be an HTTP(S) base URL without credentials, query or fragment: %w", ErrInvalid)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func integrationID(id string) bool {
	return len(id) > 0 && len(id) <= 256 && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}
