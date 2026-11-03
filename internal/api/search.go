package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

type searchInput struct {
	Query          string             `query:"q" required:"true"`
	Mode           service.SearchMode `query:"mode" enum:"fulltext,hybrid"`
	SubscriptionID int64              `query:"subscription_id" minimum:"0"`
	FolderID       int64              `query:"folder_id" minimum:"0"`
	Limit          int                `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Cursor         string             `query:"cursor" doc:"Exclusive cursor from the preceding page with the same query and filters. An empty page ends pagination."`
}
type savedSearchEntriesInput struct {
	ID     int64  `path:"id" minimum:"1"`
	Cursor string `query:"cursor"`
}
type searchMatchKind string

const (
	searchEntry     searchMatchKind = "entry"
	searchAgentNote searchMatchKind = "agent_note"
)

type searchMatch struct {
	Kind           searchMatchKind `json:"kind" enum:"entry,agent_note"`
	EntryID        int64           `json:"entry_id"`
	SubscriptionID int64           `json:"subscription_id"`
	FeedID         int64           `json:"feed_id"`
	Seq            int64           `json:"seq"`
	Title          string          `json:"title"`
	URL            string          `json:"url"`
	Snippet        string          `json:"snippet"`
	Rank           float64         `json:"rank"`
	AnnotationID   int64           `json:"annotation_id,omitempty"`
	AgentName      string          `json:"agent_name,omitempty"`
	Untrusted      bool            `json:"untrusted"`
}
type searchPage struct {
	Matches    []searchMatch `json:"matches"`
	Status     string        `json:"status,omitempty"`
	NextCursor string        `json:"next_cursor,omitempty"`
}
type searchCursor struct {
	Rank         float64 `json:"rank"`
	Seq          int64   `json:"seq"`
	AnnotationID int64   `json:"annotation_id"`
}
type savedSearch struct {
	ID      int64                `json:"id"`
	Name    string               `json:"name"`
	Query   string               `json:"query"`
	Filters service.SearchFilter `json:"filters"`
}
type savedSearchBody struct {
	Name    string               `json:"name"`
	Query   string               `json:"query"`
	Filters service.SearchFilter `json:"filters"`
}
type savedSearchRename struct {
	Name string `json:"name"`
}
type muteBody struct {
	Kind  service.MuteKind `json:"kind" enum:"keyword,author"`
	Value string           `json:"value"`
	OpID  string           `json:"op_id"`
}

func decodeSearchCursor(raw string) (*service.SearchCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 1024 {
		return nil, service.ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, service.ErrInvalid
	}
	var cursor searchCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, service.ErrInvalid
	}
	return &service.SearchCursor{Rank: cursor.Rank, Seq: cursor.Seq, AnnotationID: cursor.AnnotationID}, nil
}
func toSearchPage(rows []service.SearchResult, err error) (searchPage, error) {
	// An unavailable semantic path is an in-band status on the page, not a
	// transport failure.
	if errors.Is(err, service.ErrPipelineDisabled) {
		return searchPage{Status: statusDisabled, Matches: []searchMatch{}}, nil
	}
	if errors.Is(err, service.ErrPipelinePaused) {
		return searchPage{Status: statusPaused, Matches: []searchMatch{}}, nil
	}
	if err != nil {
		return searchPage{}, err
	}
	out := searchPage{Matches: make([]searchMatch, 0, len(rows))}
	for _, row := range rows {
		kind := searchEntry
		if row.AnnotationID != 0 {
			kind = searchAgentNote
		}
		out.Matches = append(out.Matches, searchMatch{Kind: kind, EntryID: row.EntryID, SubscriptionID: row.SubscriptionID, FeedID: row.FeedID, Seq: row.Seq, Title: row.Title, URL: row.URL, Snippet: row.Snippet, Rank: row.Rank, AnnotationID: row.AnnotationID, AgentName: row.AgentName, Untrusted: kind == searchAgentNote})
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		data, err := json.Marshal(searchCursor{Rank: last.Rank, Seq: last.Seq, AnnotationID: last.AnnotationID})
		if err != nil {
			return searchPage{}, err
		}
		out.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return out, nil
}
func toSavedSearch(row service.SavedSearch) savedSearch {
	return savedSearch{ID: row.ID, Name: row.Name, Query: row.Query, Filters: row.Filters}
}

func (s *server) searchRoutes() {
	route(s, http.MethodGet, "/v1/search", "search", func(ctx context.Context, in *searchInput) (searchPage, error) {
		cursor, err := decodeSearchCursor(in.Cursor)
		if err != nil {
			return searchPage{}, err
		}
		rows, err := s.svc.Search(ctx, actor(ctx), in.Query, service.SearchFilter{SubscriptionID: in.SubscriptionID, FolderID: in.FolderID, Mode: in.Mode, Limit: in.Limit}, cursor)
		if in.Mode == service.SearchHybrid {
			return semanticPage(rows, err)
		}
		return toSearchPage(rows, err)
	})
	route(s, http.MethodGet, "/v1/saved-searches", "list-saved-searches", func(ctx context.Context, in *empty) ([]savedSearch, error) {
		rows, err := s.svc.SavedSearches(ctx, actor(ctx))
		if err != nil {
			return nil, err
		}
		out := make([]savedSearch, 0, len(rows))
		for _, row := range rows {
			out = append(out, toSavedSearch(row))
		}
		return out, nil
	})
	route(s, http.MethodPost, "/v1/saved-searches", "create-saved-search", func(ctx context.Context, in *bodyInput[savedSearchBody]) (savedSearch, error) {
		b := in.Body
		row, err := s.svc.CreateSavedSearch(ctx, actor(ctx), b.Name, b.Query, b.Filters)
		return toSavedSearch(row), err
	})

	route(s, http.MethodGet, "/v1/saved-searches/{id}", "get-saved-search", func(ctx context.Context, in *idInput) (savedSearch, error) {
		rows, err := s.svc.SavedSearches(ctx, actor(ctx))
		if err != nil {
			return savedSearch{}, err
		}
		for _, row := range rows {
			if row.ID == in.ID {
				return toSavedSearch(row), nil
			}
		}
		return savedSearch{}, service.ErrNotFound
	})
	route(s, http.MethodPatch, "/v1/saved-searches/{id}", "rename-saved-search", func(ctx context.Context, in *idBody[savedSearchRename]) (empty, error) {
		return empty{}, s.svc.RenameSavedSearch(ctx, actor(ctx), in.ID, in.Body.Name)
	})
	route(s, http.MethodDelete, "/v1/saved-searches/{id}", "delete-saved-search", func(ctx context.Context, in *idInput) (empty, error) {
		return empty{}, s.svc.DeleteSavedSearch(ctx, actor(ctx), in.ID)
	})
	route(s, http.MethodGet, "/v1/saved-searches/{id}/entries", "saved-search-entries", func(ctx context.Context, in *savedSearchEntriesInput) (searchPage, error) {
		cursor, err := decodeSearchCursor(in.Cursor)
		if err != nil {
			return searchPage{}, err
		}
		rows, err := s.svc.ListSavedSearch(ctx, actor(ctx), in.ID, cursor)
		return toSearchPage(rows, err)
	})
	route(s, http.MethodGet, "/v1/mutes", "list-mutes", func(ctx context.Context, in *empty) ([]service.Mute, error) {
		return s.svc.Mutes(ctx, actor(ctx))
	})

	route(s, http.MethodGet, "/v1/mutes/{id}", "get-mute", func(ctx context.Context, in *idInput) (service.Mute, error) {
		rows, err := s.svc.Mutes(ctx, actor(ctx))
		if err != nil {
			return service.Mute{}, err
		}
		for _, row := range rows {
			if row.ID == in.ID {
				return row, nil
			}
		}
		return service.Mute{}, service.ErrNotFound
	})
	route(s, http.MethodPost, "/v1/mutes", "create-mute", func(ctx context.Context, in *bodyInput[muteBody]) (service.Mute, error) {
		return s.svc.CreateMute(ctx, actor(ctx), in.Body.Kind, in.Body.Value, in.Body.OpID)
	})
	route(s, http.MethodDelete, "/v1/mutes/{id}", "delete-mute", func(ctx context.Context, in *deleteInput) (empty, error) {
		return empty{}, s.svc.DeleteMute(ctx, actor(ctx), in.ID, in.OpID)
	})
}
