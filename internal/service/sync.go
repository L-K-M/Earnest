package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const (
	defaultSyncLimit = 100
	maxSyncLimit     = 1000
	// bootstrapWindow is the default cached window for entries and state when
	// a bootstrap request does not supply Since.
	bootstrapWindow = 90 * 24 * time.Hour
)

// ChangeRecord is one committed event from the user's change log. The payload
// is a syncmodel.Change field fragment, e.g. {"entry": {...}}; objects inside
// carry the change revision.
type ChangeRecord struct {
	Rev     uint64               `json:"rev"`
	Type    syncmodel.ChangeType `json:"type"`
	Payload json.RawMessage      `json:"payload"`
}

// ChangePage is one bounded slice of the change log with the cursor that
// resumes after the last returned event.
type ChangePage struct {
	Changes []ChangeRecord     `json:"changes"`
	Next    syncmodel.Revision `json:"next"`
}

// syncLimit applies the shared page bound: zero defaults, negative rejects,
// and anything above the maximum clamps.
func syncLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, fmt.Errorf("limit: %w", ErrInvalid)
	}
	if limit == 0 {
		return defaultSyncLimit, nil
	}
	return min(limit, maxSyncLimit), nil
}

// Changes returns committed events after cursor, oldest first. The cursor is
// bound to the database generation and the user's retention floor: an old
// generation, a revision below the floor or a future revision reports Gone,
// so the client re-bootstraps and replays its outbox. Validation and the page
// selection share one repeatable-read snapshot so retention or generation
// rotation cannot change the answer mid-read.
func (s *Service) Changes(ctx context.Context, a Actor, cursor syncmodel.Revision, limit int) (ChangePage, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return ChangePage{}, err
	}
	limit, err := syncLimit(limit)
	if err != nil {
		return ChangePage{}, err
	}
	page := ChangePage{Changes: []ChangeRecord{}, Next: cursor}
	err = pgx.BeginTxFunc(ctx, s.store.Pool(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			generation, rev, floor, err := store.ReadSyncCursor(ctx, tx, a.UserID)
			if err != nil {
				return err
			}
			if cursor.Generation != generation {
				return fmt.Errorf("cursor generation: %w: %w", ErrGone, syncmodel.ErrGeneration)
			}
			if cursor.Rev > uint64(rev) || cursor.Rev < uint64(floor) {
				return fmt.Errorf("cursor rev %d outside retained log: %w", cursor.Rev, ErrGone)
			}
			rows, err := store.ReadChanges(ctx, tx, a.UserID, int64(cursor.Rev), limit)
			if err != nil {
				return err
			}
			for _, row := range rows {
				page.Changes = append(page.Changes, ChangeRecord{
					Rev:     uint64(row.Rev),
					Type:    syncmodel.ChangeType(row.Type),
					Payload: row.Payload,
				})
				page.Next.Rev = uint64(row.Rev)
			}
			return nil
		})
	if err != nil {
		return ChangePage{}, err
	}
	return page, nil
}

// BootstrapCursor reads the cursor a bootstrap must capture before paging:
// the database generation and the user's current change revision, so changes
// committed during paging replay afterwards.
func (s *Service) BootstrapCursor(ctx context.Context, a Actor) (syncmodel.Revision, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return syncmodel.Revision{}, err
	}
	var cursor syncmodel.Revision
	err := pgx.BeginTxFunc(ctx, s.store.Pool(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			generation, rev, _, err := store.ReadSyncCursor(ctx, tx, a.UserID)
			if err != nil {
				return err
			}
			cursor = syncmodel.Revision{Generation: generation, Rev: uint64(rev)}
			return nil
		})
	if err != nil {
		return syncmodel.Revision{}, err
	}
	return cursor, nil
}

// BootstrapKind selects which resource a bootstrap page walks.
type BootstrapKind string

const (
	BootstrapSubscriptions    BootstrapKind = "subscriptions"
	BootstrapFolders          BootstrapKind = "folders"
	BootstrapLabels           BootstrapKind = "labels"
	BootstrapLabelMemberships BootstrapKind = "label_memberships"
	BootstrapPositions        BootstrapKind = "positions"
	BootstrapState            BootstrapKind = "state"
	BootstrapEntries          BootstrapKind = "entries"
)

// BootstrapRequest pages one bootstrap resource. AfterID is the primary
// keyset cursor; AfterPrincipal and AfterView carry the composite cursors:
// membership uses (label, entry) so AfterPrincipal is the label ID, state uses
// (principal, entry) and positions use (principal, view). Since bounds the
// cached entries/state window, defaulting to the last 90 days.
type BootstrapRequest struct {
	Kind           BootstrapKind
	AfterID        int64
	AfterPrincipal int64
	AfterView      string
	Limit          int
	Since          time.Time
}

// BootstrapPage returns one slice of the resource plus the cursor that
// resumes it. HasMore reports whether another page follows.
type BootstrapPage struct {
	Items         []json.RawMessage `json:"items"`
	NextID        int64             `json:"next_id"`
	NextPrincipal int64             `json:"next_principal"`
	NextView      string            `json:"next_view"`
	HasMore       bool              `json:"has_more"`
}

// Bootstrap pages the resources a client caches, by stable ID. Subscriptions,
// folders, labels and memberships include their tombstones; entries and state
// cover the cached window only, with entry tombstones kept inside it. No page
// carries bodies or credentials.
func (s *Service) Bootstrap(ctx context.Context, a Actor, req BootstrapRequest) (BootstrapPage, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return BootstrapPage{}, err
	}
	switch req.Kind {
	case BootstrapSubscriptions, BootstrapFolders, BootstrapLabels,
		BootstrapLabelMemberships, BootstrapPositions, BootstrapState, BootstrapEntries:
	default:
		return BootstrapPage{}, fmt.Errorf("bootstrap kind %q: %w", req.Kind, ErrInvalid)
	}
	limit, err := syncLimit(req.Limit)
	if err != nil {
		return BootstrapPage{}, err
	}
	if req.AfterID < 0 || req.AfterPrincipal < 0 {
		return BootstrapPage{}, fmt.Errorf("bootstrap cursor: %w", ErrInvalid)
	}
	since := req.Since
	if since.IsZero() {
		since = s.now().Add(-bootstrapWindow)
	}
	page := BootstrapPage{Items: []json.RawMessage{}, NextID: req.AfterID, NextPrincipal: req.AfterPrincipal, NextView: req.AfterView}
	err = pgx.BeginTxFunc(ctx, s.store.Pool(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			apply := func(p store.Page, err error) error {
				if err != nil {
					return err
				}
				page.Items, page.HasMore = p.Items, p.HasMore
				if p.ID != 0 {
					page.NextID = p.ID
				}
				if p.Principal != 0 {
					page.NextPrincipal = p.Principal
				}
				if p.View != "" {
					page.NextView = p.View
				}
				return nil
			}
			switch req.Kind {
			case BootstrapSubscriptions:
				return apply(store.PageSubscriptions(ctx, tx, a.UserID, req.AfterID, limit))
			case BootstrapFolders:
				return apply(store.PageFolders(ctx, tx, a.UserID, req.AfterID, limit))
			case BootstrapLabels:
				return apply(store.PageLabels(ctx, tx, a.UserID, req.AfterID, limit))
			case BootstrapLabelMemberships:
				return apply(store.PageLabelMemberships(ctx, tx, a.UserID, req.AfterPrincipal, req.AfterID, limit))
			case BootstrapPositions:
				return apply(store.PagePositions(ctx, tx, a.UserID, req.AfterPrincipal, req.AfterView, limit))
			case BootstrapEntries:
				return apply(store.PageEntries(ctx, tx, a.UserID, since, req.AfterID, limit))
			default:
				states, principal, entry, more, err := store.PageStates(ctx, tx, a.UserID, since, req.AfterPrincipal, req.AfterID, limit)
				if err != nil {
					return err
				}
				for _, state := range states {
					body, err := json.Marshal(state)
					if err != nil {
						return fmt.Errorf("encode state: %w", err)
					}
					page.Items = append(page.Items, body)
				}
				if len(states) > 0 {
					page.NextPrincipal, page.NextID = principal, entry
				}
				page.HasMore = more
				return nil
			}
		})
	if err != nil {
		return BootstrapPage{}, err
	}
	return page, nil
}
