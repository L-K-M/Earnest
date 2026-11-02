package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// MuteKind chooses how a mute matches river entries.
type MuteKind string

const (
	MuteKeyword MuteKind = "keyword"
	MuteAuthor  MuteKind = "author"
)

// Mute hides matching entries from the river, without changing their state.
type Mute struct {
	ID      int64    `json:"id"`
	Kind    MuteKind `json:"kind"`
	Value   string   `json:"value"`
	Rev     uint64   `json:"rev"`
	Deleted bool     `json:"deleted"`
}

// Mutes lists the user's active mutes.
func (s *Service) Mutes(ctx context.Context, a Actor) ([]Mute, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT id,kind,value,rev FROM mutes WHERE user_id=$1 AND deleted_at IS NULL ORDER BY id`, a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list mutes: %w", err)
	}
	defer rows.Close()
	out := []Mute{}
	for rows.Next() {
		var m Mute
		if err := rows.Scan(&m.ID, &m.Kind, &m.Value, &m.Rev); err != nil {
			return nil, fmt.Errorf("scan mute: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list mutes: %w", err)
	}
	return out, nil
}

// CreateMute creates a keyword or author mute. Recreating a deleted mute restores it.
func (s *Service) CreateMute(ctx context.Context, a Actor, kind MuteKind, value, opID string) (Mute, error) {
	value = strings.TrimSpace(value)
	if (kind != MuteKeyword && kind != MuteAuthor) || !validName(value) {
		return Mute{}, ErrInvalid
	}
	return s.writeMute(ctx, a, 0, kind, value, opID)
}

// DeleteMute removes a mute from the river filter.
func (s *Service) DeleteMute(ctx context.Context, a Actor, id int64, opID string) error {
	if id <= 0 {
		return ErrInvalid
	}
	_, err := s.writeMute(ctx, a, id, "", "", opID)
	return err
}

func (s *Service) writeMute(ctx context.Context, a Actor, id int64, kind MuteKind, value, opID string) (Mute, error) {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return Mute{}, err
	}
	if !validOpID(opID) {
		return Mute{}, ErrInvalid
	}
	method := "CreateMute"
	if id != 0 {
		method = "DeleteMute"
	}
	var out Mute
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, method, opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO mutes(user_id,kind,value,rev) VALUES($1,$2,$3,$4)
                ON CONFLICT(user_id,kind,value) DO UPDATE SET deleted_at=NULL,rev=EXCLUDED.rev
                WHERE mutes.deleted_at IS NOT NULL RETURNING id,kind,value,rev`, a.UserID, kind, value, rev).Scan(&out.ID, &out.Kind, &out.Value, &out.Rev)
			if err == pgx.ErrNoRows {
				return ErrConflict
			}
		} else {
			err = tx.QueryRow(ctx, `UPDATE mutes SET deleted_at=$3,rev=$4 WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL RETURNING id,kind,value,rev`, a.UserID, id, s.now().UTC(), rev).Scan(&out.ID, &out.Kind, &out.Value, &out.Rev)
			out.Deleted = true
		}
		if err != nil {
			return domainError(err)
		}
		change := syncmodel.ChangeType("mute.updated")
		if out.Deleted {
			change = "mute.deleted"
		}
		if err := s.emitChange(ctx, tx, a.UserID, rev, change, "mute", out); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	return out, domainError(err)
}
