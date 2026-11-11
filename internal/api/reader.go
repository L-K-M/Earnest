package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

type subscriptionBody struct {
	URL         string              `json:"url"`
	Title       string              `json:"title,omitempty"`
	FolderID    int64               `json:"folder_id,omitempty"`
	Cadence     service.Cadence     `json:"cadence,omitempty"`
	Extract     service.ExtractMode `json:"extract,omitempty"`
	Credentials *credentialsBody    `json:"credentials,omitempty"`
	OpID        string              `json:"op_id"`
}
type credentialsBody struct {
	Username string            `json:"username"`
	Password string            `json:"password"`
	Headers  map[string]string `json:"headers,omitempty"`
}

func credentials(c *credentialsBody) *service.FeedCredentials {
	if c == nil {
		return nil
	}
	return &service.FeedCredentials{Username: c.Username, Password: c.Password, Headers: c.Headers}
}

type subscriptionPatch struct {
	Title          *string              `json:"title,omitempty"`
	FolderID       *int64               `json:"folder_id,omitempty"`
	Cadence        *service.Cadence     `json:"cadence,omitempty"`
	Extract        *service.ExtractMode `json:"extract,omitempty"`
	Credentials    *credentialsBody     `json:"credentials,omitempty"`
	CredentialsSet bool                 `json:"credentials_set,omitempty"`
	OpID           string               `json:"op_id"`
}
type nameBody struct {
	Name string `json:"name"`
	OpID string `json:"op_id"`
}
type labelEntryInput struct {
	ID      int64 `path:"id" minimum:"1"`
	EntryID int64 `path:"entry_id" minimum:"1"`
	Body    opBody
}
type opBody struct {
	OpID string `json:"op_id"`
}

// Source is accepted for older clients but never reaches the service's trusted context.
type stateWrite struct {
	syncmodel.Write
	Source any `json:"source,omitempty"`
}
type throughBody struct {
	Write      stateWrite      `json:"write"`
	Scope      syncmodel.Scope `json:"scope"`
	ThroughSeq *uint64         `json:"through_seq,omitempty"`
	At         *time.Time      `json:"at,omitempty"`
}

type entriesInput struct {
	IDs            []int64               `query:"ids" doc:"Comma-separated entry IDs; batch results have no page cursor."`
	View           service.EntryView     `query:"view" enum:"river,later,done,kept,all"`
	Cadence        service.Cadence       `query:"cadence" enum:"now,today,week"`
	SubscriptionID int64                 `query:"subscription_id"`
	FolderID       int64                 `query:"folder_id"`
	LabelID        int64                 `query:"label_id"`
	PrincipalID    int64                 `query:"principal_id"`
	UnreadOnly     bool                  `query:"unread_only"`
	AfterSeq       uint64                `query:"after_seq"`
	BeforeSeq      uint64                `query:"before_seq"`
	Limit          int                   `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Direction      service.PageDirection `query:"direction" enum:"forward,backward"`
	Group          string                `query:"group" enum:"cluster"`
	Cursor         string                `query:"cursor" doc:"Exclusive seq cursor returned by the preceding page, preserving the same filters and direction."`
}
type entriesPage struct {
	Entries    []entry        `json:"entries"`
	Status     string         `json:"status,omitempty"`
	Groups     []clusterGroup `json:"groups,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
	// Total is the full match count for a dry run, which may exceed len(Entries).
	Total int `json:"total,omitempty"`
}

// clusterGroup annotates one listed entry with its cluster identity and how
// many other visible members share it. Unclustered entries carry zero values.
type clusterGroup struct {
	Entry     entry `json:"entry"`
	ClusterID int64 `json:"cluster_id"`
	AlsoIn    int   `json:"also_in"`
}

type folderPatchBody struct {
	Name     *string `json:"name,omitempty"`
	ParentID *int64  `json:"parent_id,omitempty" minimum:"0"`
	OpID     string  `json:"op_id"`
}

func (s *server) readerRoutes() {
	route(s, http.MethodGet, "/v1/subscriptions", "list-subscriptions", func(ctx context.Context, in *empty) ([]service.Subscription, error) {
		return s.svc.Subscriptions(ctx, actor(ctx))
	})
	route(s, http.MethodPost, "/v1/subscriptions", "subscribe", func(ctx context.Context, in *bodyInput[subscriptionBody]) (service.Subscription, error) {
		b := in.Body
		return s.svc.Subscribe(ctx, actor(ctx), b.URL, service.SubscribeOptions{Title: b.Title, FolderID: b.FolderID, Cadence: b.Cadence, Extract: b.Extract, Credentials: credentials(b.Credentials), OpID: b.OpID})
	})
	route(s, http.MethodGet, "/v1/subscriptions/{id}", "get-subscription", func(ctx context.Context, in *idInput) (service.Subscription, error) {
		return s.svc.Subscription(ctx, actor(ctx), in.ID)
	})
	route(s, http.MethodPatch, "/v1/subscriptions/{id}", "patch-subscription", func(ctx context.Context, in *idBody[subscriptionPatch]) (service.Subscription, error) {
		b := in.Body
		return s.svc.UpdateSubscription(ctx, actor(ctx), in.ID, service.UpdateSubscriptionOptions{Title: b.Title, FolderID: b.FolderID, Cadence: b.Cadence, Extract: b.Extract, Credentials: credentials(b.Credentials), CredentialsSet: b.CredentialsSet || b.Credentials != nil, OpID: b.OpID})
	})
	route(s, http.MethodDelete, "/v1/subscriptions/{id}", "unsubscribe", func(ctx context.Context, in *deleteInput) (empty, error) {
		return empty{}, s.svc.Unsubscribe(ctx, actor(ctx), in.ID, in.OpID)
	})
	route(s, http.MethodPost, "/v1/subscriptions/{id}/refresh", "refresh-subscription", func(ctx context.Context, in *idBody[opBody]) (empty, error) {
		return empty{}, s.svc.RefreshSubscription(ctx, actor(ctx), in.ID, in.Body.OpID)
	})
	s.api.OpenAPI().Paths["/v1/subscriptions/{id}/refresh"].Post.Description = "Makes a feed due for the next fetcher pass. Existing fetch leases remain in force; repeating op_id does not schedule it again."
	route(s, http.MethodGet, "/v1/feeds/{id}/health", "feed-health", func(ctx context.Context, in *idInput) (service.FeedHealth, error) {
		subscriptions, err := s.svc.Subscriptions(ctx, actor(ctx))
		if err != nil {
			return service.FeedHealth{}, err
		}
		for _, sub := range subscriptions {
			if sub.FeedID == in.ID {
				return sub.Health, nil
			}
		}
		return service.FeedHealth{}, service.ErrNotFound
	})
	route(s, http.MethodGet, "/v1/folders", "list-folders", func(ctx context.Context, in *empty) ([]service.Folder, error) { return s.svc.Folders(ctx, actor(ctx)) })
	route(s, http.MethodPost, "/v1/folders", "create-folder", func(ctx context.Context, in *bodyInput[nameBody]) (service.Folder, error) {
		return s.svc.CreateFolder(ctx, actor(ctx), in.Body.Name, in.Body.OpID)
	})
	route(s, http.MethodPatch, "/v1/folders/{id}", "rename-folder", func(ctx context.Context, in *idBody[folderPatchBody]) (service.Folder, error) {
		return s.svc.UpdateFolder(ctx, actor(ctx), in.ID, service.UpdateFolderOptions{Name: in.Body.Name, ParentID: in.Body.ParentID, OpID: in.Body.OpID})
	})
	route(s, http.MethodDelete, "/v1/folders/{id}", "delete-folder", func(ctx context.Context, in *deleteInput) (empty, error) {
		return empty{}, s.svc.DeleteFolder(ctx, actor(ctx), in.ID, in.OpID)
	})
	route(s, http.MethodGet, "/v1/entries", "list-entries", func(ctx context.Context, in *entriesInput) (entriesPage, error) {
		if len(in.IDs) > 0 {
			entries, err := s.svc.GetByIDs(ctx, actor(ctx), in.IDs)
			return entriesPage{Entries: toEntries(entries)}, err
		}
		if in.Cursor != "" {
			seq, err := strconv.ParseUint(in.Cursor, 10, 64)
			if err != nil {
				return entriesPage{}, service.ErrInvalid
			}
			if in.Direction == service.PageBackward {
				in.BeforeSeq = seq
			} else {
				in.AfterSeq = seq
			}
		}
		filter := service.EntryFilter{View: in.View, Cadence: in.Cadence, SubscriptionID: in.SubscriptionID, FolderID: in.FolderID, LabelID: in.LabelID, PrincipalID: in.PrincipalID, UnreadOnly: in.UnreadOnly, AfterSeq: in.AfterSeq, BeforeSeq: in.BeforeSeq, Limit: in.Limit, Direction: in.Direction}
		if in.Group == "cluster" {
			groups, err := s.svc.ClusterGroups(ctx, actor(ctx), filter)
			if err != nil {
				return entriesPage{}, pipelineErr(err)
			}
			entries := make([]service.Entry, 0, len(groups))
			page := entriesPage{Groups: make([]clusterGroup, 0, len(groups))}
			status, err := s.svc.PipelineStatus(ctx, actor(ctx))
			if err != nil {
				return entriesPage{}, err
			}
			page.Status = status.Embed
			for _, g := range groups {
				entries = append(entries, g.Entry)
				page.Groups = append(page.Groups, clusterGroup{Entry: toEntry(g.Entry), ClusterID: g.ClusterID, AlsoIn: g.AlsoIn})
			}
			page.Entries = toEntries(entries)
			if len(groups) == in.Limit {
				page.NextCursor = strconv.FormatUint(entries[len(entries)-1].Seq, 10)
			}
			return page, nil
		}
		entries, err := s.svc.ClusterEntries(ctx, actor(ctx), filter)
		page := entriesPage{Entries: toEntries(entries)}
		if len(entries) == in.Limit {
			page.NextCursor = strconv.FormatUint(entries[len(entries)-1].Seq, 10)
		}
		return page, err
	})
	route(s, http.MethodGet, "/v1/entries:counts", "entry-counts", func(ctx context.Context, in *empty) (service.UnreadCounts, error) {
		return s.svc.UnreadCounts(ctx, actor(ctx))
	})
	route(s, http.MethodGet, "/v1/entries/{id}", "get-entry", func(ctx context.Context, in *idInput) (entry, error) {
		result, err := s.svc.Entry(ctx, actor(ctx), in.ID)
		return toEntry(result), err
	})
	route(s, http.MethodGet, "/v1/entries/{id}/content", "entry-content", func(ctx context.Context, in *idInput) (content, error) {
		result, err := s.svc.Content(ctx, actor(ctx), in.ID)
		return content(result), err
	})
	route(s, http.MethodPost, "/v1/state:batch", "state-batch", func(ctx context.Context, in *bodyInput[[]stateWrite]) ([]syncmodel.Result, error) {
		writes := make([]syncmodel.Write, len(in.Body))
		for i, w := range in.Body {
			writes[i] = w.Write
		}
		return s.svc.Batch(ctx, actor(ctx), writes)
	})
	route(s, http.MethodPost, "/v1/state:through", "state-through", func(ctx context.Context, in *bodyInput[throughBody]) (syncmodel.RangeResult, error) {
		b := in.Body
		return s.svc.Through(ctx, actor(ctx), service.ThroughRequest{Write: b.Write.Write, Scope: b.Scope, ThroughSeq: b.ThroughSeq, At: b.At})
	})
	route(s, http.MethodGet, "/v1/positions", "list-positions", func(ctx context.Context, in *empty) ([]syncmodel.Position, error) {
		return s.svc.Positions(ctx, actor(ctx))
	})
	route(s, http.MethodGet, "/v1/positions/{view}", "get-position", func(ctx context.Context, in *struct {
		View string `path:"view"`
	}) (syncmodel.Position, error) {
		return s.svc.Position(ctx, actor(ctx), in.View)
	})
	route(s, http.MethodPut, "/v1/positions/{view}", "put-position", func(ctx context.Context, in *struct {
		View string `path:"view"`
		Body struct {
			Seq  uint64 `json:"seq"`
			OpID string `json:"op_id"`
		}
	}) (syncmodel.Position, error) {
		return s.svc.PutPosition(ctx, actor(ctx), in.View, in.Body.Seq, in.Body.OpID)
	})
	route(s, http.MethodGet, "/v1/labels", "list-labels", func(ctx context.Context, in *empty) ([]service.Label, error) { return s.svc.Labels(ctx, actor(ctx)) })
	route(s, http.MethodPost, "/v1/labels", "create-label", func(ctx context.Context, in *bodyInput[nameBody]) (service.Label, error) {
		return s.svc.CreateLabel(ctx, actor(ctx), in.Body.Name, in.Body.OpID)
	})
	route(s, http.MethodPatch, "/v1/labels/{id}", "rename-label", func(ctx context.Context, in *idBody[nameBody]) (service.Label, error) {
		return s.svc.RenameLabel(ctx, actor(ctx), in.ID, in.Body.Name, in.Body.OpID)
	})
	route(s, http.MethodDelete, "/v1/labels/{id}", "delete-label", func(ctx context.Context, in *deleteInput) (service.Label, error) {
		return s.svc.DeleteLabel(ctx, actor(ctx), in.ID, in.OpID)
	})
	route(s, http.MethodPut, "/v1/labels/{id}/entries/{entry_id}", "add-label", func(ctx context.Context, in *labelEntryInput) (service.LabelMembership, error) {
		return s.svc.AddLabel(ctx, actor(ctx), in.ID, in.EntryID, in.Body.OpID)
	})
	route(s, http.MethodDelete, "/v1/labels/{id}/entries/{entry_id}", "remove-label", func(ctx context.Context, in *struct {
		ID      int64  `path:"id" minimum:"1"`
		EntryID int64  `path:"entry_id" minimum:"1"`
		OpID    string `query:"op_id" required:"true"`
	}) (service.LabelMembership, error) {
		return s.svc.RemoveLabel(ctx, actor(ctx), in.ID, in.EntryID, strings.TrimSpace(in.OpID))
	})
}
