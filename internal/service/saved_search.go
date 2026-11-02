package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SavedSearch is a named query and filter, evaluated when opened.
type SavedSearch struct {
	ID      int64
	Name    string
	Query   string
	Filters SearchFilter
}

// CreateSavedSearch creates a named view owned by the caller's user.
func (s *Service) CreateSavedSearch(ctx context.Context, a Actor, name, query string, f SearchFilter) (SavedSearch, error) {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return SavedSearch{}, err
	}
	if !validName(name) || strings.TrimSpace(query) == "" || len(query) > 4096 || f.SubscriptionID < 0 || f.FolderID < 0 || f.Limit < 0 || f.Limit > maxEntryLimit {
		return SavedSearch{}, fmt.Errorf("saved search: %w", ErrInvalid)
	}
	body, err := json.Marshal(f)
	if err != nil {
		return SavedSearch{}, fmt.Errorf("encode filters: %w", err)
	}
	out := SavedSearch{Name: name, Query: query, Filters: f}
	err = s.store.Pool().QueryRow(ctx, `INSERT INTO saved_searches(user_id,name,query,filters) VALUES($1,$2,$3,$4) RETURNING id`, a.UserID, name, query, body).Scan(&out.ID)
	if err != nil {
		return SavedSearch{}, domainError(err)
	}
	return out, nil
}

// SavedSearches lists the user's active saved views.
func (s *Service) SavedSearches(ctx context.Context, a Actor) ([]SavedSearch, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT id,name,query,filters FROM saved_searches WHERE user_id=$1 AND deleted_at IS NULL ORDER BY id`, a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	defer rows.Close()
	out := []SavedSearch{}
	for rows.Next() {
		var v SavedSearch
		var data []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Query, &data); err != nil {
			return nil, fmt.Errorf("scan saved search: %w", err)
		}
		if err := json.Unmarshal(data, &v.Filters); err != nil {
			return nil, fmt.Errorf("decode saved search filters: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	return out, nil
}

// RenameSavedSearch changes a saved view's name.
func (s *Service) RenameSavedSearch(ctx context.Context, a Actor, id int64, name string) error {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return err
	}
	if id <= 0 || !validName(name) {
		return ErrInvalid
	}
	tag, err := s.store.Pool().Exec(ctx, `UPDATE saved_searches SET name=$3 WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, a.UserID, id, name)
	if err != nil {
		return domainError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSavedSearch removes a named view.
func (s *Service) DeleteSavedSearch(ctx context.Context, a Actor, id int64) error {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return err
	}
	if id <= 0 {
		return ErrInvalid
	}
	tag, err := s.store.Pool().Exec(ctx, `DELETE FROM saved_searches WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, a.UserID, id)
	if err != nil {
		return domainError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSavedSearch evaluates a saved view against the current entries.
func (s *Service) ListSavedSearch(ctx context.Context, a Actor, id int64, cursor *SearchCursor) ([]SearchResult, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, ErrInvalid
	}
	var query string
	var body []byte
	if err := s.store.Pool().QueryRow(ctx, `SELECT query,filters FROM saved_searches WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, a.UserID, id).Scan(&query, &body); err != nil {
		return nil, domainError(err)
	}
	var filter SearchFilter
	if err := json.Unmarshal(body, &filter); err != nil {
		return nil, fmt.Errorf("decode saved search filters: %w", err)
	}
	return s.Search(ctx, a, query, filter, cursor)
}
