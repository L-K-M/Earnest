package service

import (
	"context"
	"fmt"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const maxOPMLOutlines = 10000
const maxOPMLDepth = 64

// OPMLOutline is the transport-independent OPML tree, including nested folders.
type OPMLOutline struct {
	Title    string        `json:"title"`
	XMLURL   string        `json:"xmlUrl,omitempty"`
	HTMLURL  string        `json:"htmlUrl,omitempty"`
	Outlines []OPMLOutline `json:"outlines,omitempty"`
}

// OPMLResult reports each input outline independently. Deferred feeds are due
// now, and are fetched through the normal leased scheduler without a probe burst.
type OPMLResult struct {
	Path         []int         `json:"path"`
	Subscription *Subscription `json:"subscription,omitempty"`
	Folder       *Folder       `json:"folder,omitempty"`
	Deferred     bool          `json:"deferred"`
	Error        string        `json:"error,omitempty"`
}

// ImportOPML imports a tree without probing. Repeating the same tree reuses
// existing folders/subscriptions, and per-outline failures do not abort siblings.
func (s *Service) ImportOPML(ctx context.Context, a Actor, outlines []OPMLOutline) ([]OPMLResult, error) {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return nil, err
	}
	count := 0
	var validate func([]OPMLOutline, int) error
	validate = func(items []OPMLOutline, depth int) error {
		if depth > maxOPMLDepth {
			return ErrInvalid
		}
		for _, item := range items {
			count++
			if count > maxOPMLOutlines {
				return ErrInvalid
			}
			if err := validate(item.Outlines, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(outlines, 0); err != nil {
		return nil, fmt.Errorf("OPML bounds: %w", err)
	}
	results := []OPMLResult{}
	var visit func([]OPMLOutline, int64, []int) error
	visit = func(items []OPMLOutline, folderID int64, parent []int) error {
		for i, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			path := append(append([]int{}, parent...), i)
			result := OPMLResult{Path: path}
			nextFolder := folderID
			if item.XMLURL != "" {
				rawURL, err := normalizedFeedURL(item.XMLURL)
				if err != nil {
					result.Error = "invalid feed URL"
					results = append(results, result)
					continue
				}
				if len(item.Title) > 1024 {
					result.Error = "title too long"
					results = append(results, result)
					continue
				}
				var sub Subscription
				var created bool
				err = s.withEvents(ctx, func(tx pgx.Tx) error {
					if err := store.LockCore(ctx, tx); err != nil {
						return err
					}
					var err error
					sub, created, err = s.subscribeTx(ctx, tx, a, rawURL, FeedMeta{Title: item.Title, SiteURL: item.HTMLURL}, SubscribeOptions{Title: item.Title, FolderID: folderID}, nil)
					return err
				})
				if err != nil {
					result.Error = domainError(err).Error()
				} else {
					result.Subscription = &sub
					// Only a new subscription schedules its first fetch; an
					// already-subscribed feed is not deferred work.
					result.Deferred = created
				}
			} else if item.Title != "" {
				folder, err := s.importFolder(ctx, a, item.Title)
				if err != nil {
					result.Error = domainError(err).Error()
				} else {
					result.Folder = &folder
					nextFolder = folder.ID
				}
			}
			results = append(results, result)
			if err := visit(item.Outlines, nextFolder, path); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(outlines, 0, nil); err != nil {
		return results, err
	}
	return results, nil
}

func (s *Service) importFolder(ctx context.Context, a Actor, name string) (Folder, error) {
	if !validName(name) {
		return Folder{}, ErrInvalid
	}
	var folder Folder
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, "SELECT id,name,rev FROM folders WHERE user_id=$1 AND name=$2 AND deleted_at IS NULL", a.UserID, name).Scan(&folder.ID, &folder.Name, &folder.Rev)
		if err == nil {
			return nil
		}
		if err != pgx.ErrNoRows {
			return err
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "INSERT INTO folders(user_id,name,rev) VALUES($1,$2,$3) RETURNING id", a.UserID, name, rev).Scan(&folder.ID); err != nil {
			return err
		}
		folder.Name = name
		folder.Rev = uint64(rev)
		return s.emitChange(ctx, tx, a.UserID, rev, syncmodel.FolderUpdated, "folder", folder)
	})
	return folder, err
}

// ExportOPML returns the active subscriptions in stable folder/ID order.
func (s *Service) ExportOPML(ctx context.Context, a Actor) ([]OPMLOutline, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	folders, err := s.Folders(ctx, a)
	if err != nil {
		return nil, err
	}
	subscriptions, err := s.Subscriptions(ctx, a)
	if err != nil {
		return nil, err
	}
	out := []OPMLOutline{}
	byFolder := map[int64]int{}
	for _, folder := range folders {
		byFolder[folder.ID] = len(out)
		out = append(out, OPMLOutline{Title: folder.Name, Outlines: []OPMLOutline{}})
	}
	for _, sub := range subscriptions {
		outline := OPMLOutline{Title: sub.Title, XMLURL: sub.URL, HTMLURL: sub.SiteURL}
		if i, ok := byFolder[sub.FolderID]; ok {
			out[i].Outlines = append(out[i].Outlines, outline)
		} else {
			out = append(out, outline)
		}
	}
	return out, nil
}
