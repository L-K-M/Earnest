package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const maxOPMLOutlines = 10000
const maxOPMLDepth = 64

// opmlChunkSize bounds how many outlines one import transaction covers. The
// global sequence lock is released between chunks, so a large import cannot
// block every other writer for its whole duration.
const opmlChunkSize = 100

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

// opmlWork is one flattened outline in depth-first order. parent is the index
// of the nearest ancestor folder, or -1.
type opmlWork struct {
	path    []int
	title   string
	xmlURL  string
	htmlURL string
	folder  bool
	parent  int
}

// ImportOPML imports a tree without probing. Repeating the same tree reuses
// existing folders/subscriptions, and per-outline failures do not abort
// siblings. Work is committed in bounded chunks so the global sequence lock is
// released between chunks and a large import does not block every other writer
// for its whole duration. A savepoint per outline keeps one bad row from
// aborting its chunk.
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
	works := []opmlWork{}
	var flatten func(items []OPMLOutline, parent int, parentPath []int)
	flatten = func(items []OPMLOutline, parent int, parentPath []int) {
		for i, item := range items {
			path := append(append([]int{}, parentPath...), i)
			w := opmlWork{path: path, title: item.Title, xmlURL: item.XMLURL, htmlURL: item.HTMLURL, parent: parent}
			if item.XMLURL == "" && item.Title != "" {
				w.folder = true
			}
			index := len(works)
			works = append(works, w)
			childParent := parent
			if w.folder {
				childParent = index
			}
			flatten(item.Outlines, childParent, path)
		}
	}
	flatten(outlines, -1, nil)

	results := make([]OPMLResult, len(works))
	for start := 0; start < len(works); start += opmlChunkSize {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		end := min(start+opmlChunkSize, len(works))
		err := s.withEvents(ctx, func(tx pgx.Tx) error {
			if err := store.LockCore(ctx, tx); err != nil {
				return err
			}
			for i := start; i < end; i++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				w := works[i]
				if _, err := tx.Exec(ctx, "SAVEPOINT earnest_opml_item"); err != nil {
					return err
				}
				result, itemErr := s.importOPMLItem(ctx, tx, a, w, folderIDFor(works, results, w))
				if itemErr != nil {
					if _, rollbackErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT earnest_opml_item"); rollbackErr != nil {
						return rollbackErr
					}
					results[i] = OPMLResult{Path: w.path, Error: domainError(itemErr).Error()}
				} else {
					results[i] = result
				}
				if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT earnest_opml_item"); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

// folderIDFor resolves the folder a work item belongs to, walking past folders
// whose creation failed in this or an earlier chunk.
func folderIDFor(works []opmlWork, results []OPMLResult, w opmlWork) int64 {
	for i := w.parent; i >= 0; i = works[i].parent {
		if results[i].Folder != nil {
			return results[i].Folder.ID
		}
	}
	return 0
}

func (s *Service) importOPMLItem(ctx context.Context, tx pgx.Tx, a Actor, w opmlWork, folderID int64) (OPMLResult, error) {
	result := OPMLResult{Path: w.path}
	if w.xmlURL != "" {
		rawURL, err := normalizedFeedURL(w.xmlURL)
		if err != nil {
			return result, errors.New("invalid feed URL")
		}
		if len(w.title) > 1024 {
			return result, errors.New("title too long")
		}
		sub, err := s.subscribeTx(ctx, tx, a, rawURL, FeedMeta{Title: w.title, SiteURL: w.htmlURL}, SubscribeOptions{Title: w.title, FolderID: folderID}, nil)
		if err != nil {
			return result, err
		}
		result.Subscription = &sub
		result.Deferred = true
		return result, nil
	}
	if w.folder {
		folder, err := s.importFolderTx(ctx, tx, a, w.title)
		if err != nil {
			return result, err
		}
		result.Folder = &folder
	}
	return result, nil
}

func (s *Service) importFolderTx(ctx context.Context, tx pgx.Tx, a Actor, name string) (Folder, error) {
	if !validName(name) {
		return Folder{}, ErrInvalid
	}
	var folder Folder
	err := tx.QueryRow(ctx, "SELECT id,name,rev FROM folders WHERE user_id=$1 AND name=$2 AND deleted_at IS NULL", a.UserID, name).Scan(&folder.ID, &folder.Name, &folder.Rev)
	if err == nil {
		return folder, nil
	}
	if err != pgx.ErrNoRows {
		return Folder{}, err
	}
	rev, err := changeRev(ctx, tx, a.UserID)
	if err != nil {
		return Folder{}, err
	}
	if err := tx.QueryRow(ctx, "INSERT INTO folders(user_id,name,rev) VALUES($1,$2,$3) RETURNING id", a.UserID, name, rev).Scan(&folder.ID); err != nil {
		return Folder{}, err
	}
	folder.Name = name
	folder.Rev = uint64(rev)
	return folder, s.emitChange(ctx, tx, a.UserID, rev, syncmodel.FolderUpdated, "folder", folder)
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
