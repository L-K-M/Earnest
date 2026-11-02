package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// Positions returns every resume position the actor's principal holds.
func (s *Service) Positions(ctx context.Context, a Actor) ([]syncmodel.Position, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	return store.LoadPositions(ctx, s.store.Pool(), a.PrincipalID)
}

// Position returns one view's anchor. A view never written reports seq 0,
// the start of the stream, not an error.
func (s *Service) Position(ctx context.Context, a Actor, view string) (syncmodel.Position, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return syncmodel.Position{}, err
	}
	if !validName(view) {
		return syncmodel.Position{}, fmt.Errorf("view: %w", ErrInvalid)
	}
	p, err := store.LoadPosition(ctx, s.store.Pool(), a.PrincipalID, view)
	if errors.Is(err, pgx.ErrNoRows) {
		return syncmodel.Position{Principal: syncmodel.ID(a.PrincipalID), View: view}, nil
	}
	if err != nil {
		return syncmodel.Position{}, err
	}
	return p, nil
}

// PutPosition moves the actor's own resume anchor. Positions are last-writer-
// wins including backward moves, sync live through position.updated, and
// never mark anything seen.
func (s *Service) PutPosition(ctx context.Context, a Actor, view string, seq uint64, opID string) (syncmodel.Position, error) {
	// Humans move their position through state:write; an agent may only ever
	// move its own through position:own.
	scope := ScopeStateWrite
	if a.Kind == PrincipalAgent {
		scope = ScopePositionOwn
	}
	if err := s.checkActor(ctx, a, scope); err != nil {
		return syncmodel.Position{}, err
	}
	if !validName(view) {
		return syncmodel.Position{}, fmt.Errorf("view: %w", ErrInvalid)
	}
	if seq > math.MaxInt64 {
		return syncmodel.Position{}, fmt.Errorf("seq beyond signed bigint: %w", ErrInvalid)
	}
	if !validOpID(opID) {
		return syncmodel.Position{}, fmt.Errorf("op_id: %w", ErrInvalid)
	}
	out := syncmodel.Position{Principal: syncmodel.ID(a.PrincipalID), View: view, Seq: seq}
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "PutPosition", opID)
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
		if err := store.UpsertPosition(ctx, tx, a.PrincipalID, view, seq, rev); err != nil {
			return domainError(err)
		}
		out.Rev = uint64(rev)
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.PositionUpdated, "position", out); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return syncmodel.Position{}, err
	}
	return out, nil
}
