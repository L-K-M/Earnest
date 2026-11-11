package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// Folder groups subscriptions. Deleted folders survive as revisioned tombstones.
// ParentID is optional in the schema so generated clients can read older servers.
type Folder struct {
	ID       int64  `json:"id"`
	ParentID int64  `json:"parent_id" required:"false"`
	Name     string `json:"name"`
	Rev      uint64 `json:"rev"`
	Deleted  bool   `json:"deleted"`
}

// Folders returns active folders ordered by stable ID.
func (s *Service) Folders(ctx context.Context, a Actor) ([]Folder, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, "SELECT id,name,rev,COALESCE(parent_id,0) FROM folders WHERE user_id=$1 AND deleted_at IS NULL ORDER BY id", a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var folder Folder
		if err := rows.Scan(&folder.ID, &folder.Name, &folder.Rev, &folder.ParentID); err != nil {
			return nil, err
		}
		out = append(out, folder)
	}
	return out, rows.Err()
}

// CreateFolder creates a named folder, deduplicated by the actor's operation ID.
func (s *Service) CreateFolder(ctx context.Context, a Actor, name, opID string) (Folder, error) {
	return s.writeFolder(ctx, a, 0, name, opID, folderCreate, nil)
}

// RenameFolder changes one active folder's name.
func (s *Service) RenameFolder(ctx context.Context, a Actor, id int64, name, opID string) (Folder, error) {
	if id <= 0 {
		return Folder{}, ErrInvalid
	}
	return s.writeFolder(ctx, a, id, name, opID, folderRename, nil)
}

// UpdateFolderOptions changes only the supplied fields. ParentID zero means root.
type UpdateFolderOptions struct {
	Name     *string
	ParentID *int64
	OpID     string
}

func (s *Service) UpdateFolder(ctx context.Context, a Actor, id int64, opts UpdateFolderOptions) (Folder, error) {
	if id <= 0 || (opts.Name == nil && opts.ParentID == nil) || (opts.Name != nil && !validName(*opts.Name)) || (opts.ParentID != nil && *opts.ParentID < 0) {
		return Folder{}, ErrInvalid
	}
	// Preserve the existing rename operation identity for old clients/retries.
	if opts.ParentID == nil {
		return s.RenameFolder(ctx, a, id, *opts.Name, opts.OpID)
	}
	name := ""
	if opts.Name != nil {
		name = *opts.Name
	}
	return s.writeFolder(ctx, a, id, name, opts.OpID, folderUpdate, opts.ParentID)
}

type folderAction string

const (
	folderCreate folderAction = "CreateFolder"
	folderRename folderAction = "RenameFolder"
	folderDelete folderAction = "DeleteFolder"
	folderUpdate folderAction = "UpdateFolder"
)

func (s *Service) writeFolder(ctx context.Context, a Actor, id int64, name, opID string, method folderAction, parentID *int64) (Folder, error) {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return Folder{}, err
	}
	if !validOpID(opID) || ((method == folderCreate || method == folderRename) && !validName(name)) {
		return Folder{}, ErrInvalid
	}
	var out Folder
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, string(method), opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		if id != 0 {
			if err := tx.QueryRow(ctx, "SELECT id,name,rev,COALESCE(parent_id,0) FROM folders WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL", a.UserID, id).Scan(&out.ID, &out.Name, &out.Rev, &out.ParentID); err != nil {
				return domainError(err)
			}
		}
		if parentID != nil && *parentID != 0 {
			if err := ensureFolder(ctx, tx, a.UserID, *parentID); err != nil {
				return err
			}
			var cycle bool
			// LockCore serializes this check with every move and deletion.
			err := tx.QueryRow(ctx, `WITH RECURSIVE ancestors AS (
                SELECT id,parent_id FROM folders WHERE user_id=$1 AND id=$2
                UNION
                SELECT f.id,f.parent_id FROM folders f JOIN ancestors p ON f.id=p.parent_id WHERE f.user_id=$1
            ) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=$3)`, a.UserID, *parentID, id).Scan(&cycle)
			if err != nil {
				return err
			}
			if cycle {
				return ErrInvalid
			}
		}

		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		switch method {
		case folderCreate:
			if err := tx.QueryRow(ctx, "INSERT INTO folders(user_id,name,rev) VALUES($1,$2,$3) RETURNING id", a.UserID, name, rev).Scan(&out.ID); err != nil {
				return domainError(err)
			}
			out.Name = name
		case folderRename:
			if _, err := tx.Exec(ctx, "UPDATE folders SET name=$2,rev=$3 WHERE id=$1", id, name, rev); err != nil {
				return domainError(err)
			}
			out.Name = name
		case folderUpdate:
			if name != "" {
				out.Name = name
			}
			out.ParentID = *parentID
			if _, err := tx.Exec(ctx, "UPDATE folders SET name=$2,parent_id=NULLIF($3,0),rev=$4 WHERE id=$1", id, out.Name, out.ParentID, rev); err != nil {
				return domainError(err)
			}
		case folderDelete:
			// Each affected subscription gets its own revisioned move event.
			rows, err := tx.Query(ctx, "SELECT id FROM subscriptions WHERE user_id=$1 AND folder_id=$2 AND deleted_at IS NULL ORDER BY id", a.UserID, id)
			if err != nil {
				return err
			}
			ids := []int64{}
			for rows.Next() {
				var sub int64
				if err := rows.Scan(&sub); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, sub)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			// The folder event uses the already allocated first revision; the moves
			// follow it in the same atomic transaction.
			if _, err := tx.Exec(ctx, "UPDATE folders SET deleted_at=$2,rev=$3 WHERE id=$1", id, s.now().UTC(), rev); err != nil {
				return err
			}
			out.Deleted = true
			out.Rev = uint64(rev)
			if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.FolderDeleted, "folder", out); err != nil {
				return err
			}
			for _, subID := range ids {
				moveRev, err := changeRev(ctx, tx, a.UserID)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "UPDATE subscriptions SET folder_id=NULL,rev=$2 WHERE id=$1", subID, moveRev); err != nil {
					return err
				}
				sub, err := subscriptionTx(ctx, tx, a.UserID, subID)
				if err != nil {
					return err
				}
				if err := s.emitChange(ctx, tx, a.UserID, moveRev, syncmodel.SubscriptionUpdated, "subscription", sub); err != nil {
					return err
				}
			}
			// Keep child folders and their feeds; each root move is visible to sync.
			children, err := tx.Query(ctx, "SELECT id,name FROM folders WHERE user_id=$1 AND parent_id=$2 AND deleted_at IS NULL ORDER BY id", a.UserID, id)
			if err != nil {
				return err
			}
			childFolders, err := pgx.CollectRows(children, func(row pgx.CollectableRow) (Folder, error) {
				var child Folder
				err := row.Scan(&child.ID, &child.Name)
				return child, err
			})
			if err != nil {
				return err
			}
			for _, child := range childFolders {
				childRev, err := changeRev(ctx, tx, a.UserID)
				if err != nil {
					return err
				}
				child.Rev = uint64(childRev)
				if _, err := tx.Exec(ctx, "UPDATE folders SET parent_id=NULL,rev=$2 WHERE id=$1", child.ID, childRev); err != nil {
					return err
				}
				if err := s.emitChange(ctx, tx, a.UserID, childRev, syncmodel.FolderUpdated, "folder", child); err != nil {
					return err
				}
			}
			return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
		}
		out.Rev = uint64(rev)
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.FolderUpdated, "folder", out); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	return out, domainError(err)
}

// DeleteFolder tombstones a folder and moves its subscriptions to no folder.
func (s *Service) DeleteFolder(ctx context.Context, a Actor, id int64, opID string) error {
	if id <= 0 {
		return ErrInvalid
	}
	_, err := s.writeFolder(ctx, a, id, "", opID, folderDelete, nil)
	return err
}
