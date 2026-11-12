package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

type SendArticleResult struct {
	Message string `json:"message"`
}

// SendArticle sends only on an explicit human action. It never follows a
// redirect with a saved credential or gives an agent access to that credential.
func (s *Service) SendArticle(ctx context.Context, a Actor, id int64, provider ArticleIntegration, opID string) (SendArticleResult, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return SendArticleResult{}, err
	}
	if !provider.valid() || !validOpID(opID) || id <= 0 {
		return SendArticleResult{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entry, err := s.Entry(ctx, a, id)
	if err != nil {
		return SendArticleResult{}, err
	}
	u, err := url.Parse(entry.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return SendArticleResult{}, fmt.Errorf("this article has no HTTP(S) URL to send: %w", ErrInvalid)
	}
	configs, err := s.loadArticleIntegrations(ctx, a.UserID)
	if err != nil {
		return SendArticleResult{}, err
	}
	config := configs[provider]
	if config.URL == "" {
		return SendArticleResult{}, fmt.Errorf("configure %s in Settings first: %w", provider, ErrInvalid)
	}
	var token string
	if len(config.Token) > 0 {
		plain, err := s.openSecret(config.Token, provider.tokenPurpose(a.UserID))
		if err != nil {
			return SendArticleResult{}, err
		}
		token = string(plain)
	}
	if s.outbound == nil {
		return SendArticleResult{}, fmt.Errorf("outbound client: %w", ErrUnavailable)
	}
	var text string
	if provider == IntegrationHister {
		body, err := s.Content(ctx, a, id)
		if err != nil {
			return SendArticleResult{}, err
		}
		text = body.Text
		if len(text) > 8<<20 {
			return SendArticleResult{}, fmt.Errorf("article text exceeds Hister send limit of 8 MiB: %w", ErrInvalid)
		}
	}
	var out SendArticleResult
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		// The unique op envelope serializes repeats without holding LockCore
		// over network I/O. Both destinations also deduplicate by URL, so an
		// explicit retry after an uncertain network/commit failure is safe.
		method := "SendArticle/" + string(provider) + "/" + strconv.FormatInt(id, 10)
		replay, err := replayOp(ctx, tx, a.PrincipalID, method, opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		switch provider {
		case IntegrationKarakeep:
			var bookmark struct {
				ID string `json:"id"`
			}
			// Karakeep retrieves the original; only the URL is needed. Omitting
			// title avoids overriding its own metadata and title-length limits.
			if err := s.integrationRequest(ctx, provider, config.URL+"/api/v1/bookmarks", token, http.MethodPost, map[string]string{"type": "link", "url": entry.URL}, &bookmark, http.StatusOK, http.StatusCreated); err != nil {
				return err
			}
			if !integrationID(bookmark.ID) {
				return fmt.Errorf("Karakeep returned no valid bookmark ID; check the server URL before retrying: %w", ErrUnavailable)
			}
			if config.DefaultListID != "" {
				endpoint := config.URL + "/api/v1/lists/" + url.PathEscape(config.DefaultListID) + "/bookmarks/" + url.PathEscape(bookmark.ID)
				if err := s.integrationRequest(ctx, provider, endpoint, token, http.MethodPut, nil, nil, http.StatusNoContent); err != nil {
					return fmt.Errorf("saved to Karakeep, but adding it to the default list could not be confirmed; check the list ID and retry: %w", err)
				}
			}
			out.Message = "Sent to Karakeep."
		case IntegrationHister:
			payload := map[string]string{"url": entry.URL, "title": entry.Title, "text": text}
			if config.DefaultLabel != "" {
				payload["label"] = config.DefaultLabel
			}
			if err := s.integrationRequest(ctx, provider, config.URL+"/api/add", token, http.MethodPost, payload, nil, http.StatusCreated); err != nil {
				return err
			}
			out.Message = "Sent to Hister."
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return SendArticleResult{}, err
	}
	return out, nil
}

func (s *Service) integrationRequest(ctx context.Context, provider ArticleIntegration, endpoint, token, method string, payload, response any, statuses ...int) error {
	var data []byte
	if payload != nil {
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("integration request: %w", ErrInvalid)
	}
	req.Header.Set("Accept", "application/json")
	if provider == IntegrationHister {
		// Hister requires its documented command-line Origin even for
		// token-authenticated, server-to-server requests.
		req.Header.Set("Origin", "hister://")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	result, err := s.outbound.Do(req, outbound.Policy{
		Purpose: outbound.Integration, Redirect: outbound.RedirectNone,
		Credentials: outbound.Credentials{Headers: headers}, ExtraPorts: s.mediaPorts,
		MaxBodySize: 1 << 20, Timeout: 20 * time.Second,
	})
	if result != nil && result.Response != nil {
		defer result.Response.Body.Close()
	}
	if err != nil {
		if outbound.ErrorClass(err) == outbound.DeniedAddress {
			return fmt.Errorf("integration destination blocked; allow its exact host:port with EARNEST_OUTBOUND_ALLOW_PRIVATE for a private server: %w", ErrUnavailable)
		}
		if result == nil || result.Response == nil {
			return fmt.Errorf("could not confirm delivery; the article may have been saved. Check the destination before retrying: %w", ErrUnavailable)
		}
	}
	if result == nil || result.Response == nil {
		return ErrUnavailable
	}
	status := result.Response.StatusCode
	if err != nil || !slices.Contains(statuses, status) {
		// Never include a third-party response body: it can echo credentials
		// or contain untrusted HTML. A login page/redirect is not a success.
		return fmt.Errorf("integration returned HTTP %d; check its URL, API token and default destination: %w", status, ErrUnavailable)
	}
	if response != nil {
		if !strings.Contains(strings.ToLower(result.Response.Header.Get("Content-Type")), "application/json") || json.NewDecoder(result.Response.Body).Decode(response) != nil {
			return fmt.Errorf("integration returned an invalid response; check its base URL before retrying: %w", ErrUnavailable)
		}
	}
	return nil
}
