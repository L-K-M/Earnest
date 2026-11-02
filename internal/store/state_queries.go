package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// StateReader is implemented by a pool and a transaction.
type StateReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// LoadState returns the model's default registers for an absent row.
// Mutators hold LockCore before reading; readers may use a snapshot transaction.
func LoadState(ctx context.Context, q StateReader, principal, entry int64) (syncmodel.State, error) {
	s := syncmodel.InitialState(syncmodel.ID(principal), syncmodel.ID(entry))
	var seen, shelf string
	var kept bool
	var times [3]*time.Time
	var actors [3]*int64
	var sources [3]*string
	err := q.QueryRow(ctx, `SELECT seen,seen_rev,seen_occurred_at,seen_actor,seen_source,
 shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,
 kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev
 FROM entry_state WHERE principal_id=$1 AND entry_id=$2`, principal, entry).Scan(
		&seen, &s.Seen.Rev, &times[0], &actors[0], &sources[0],
		&shelf, &s.Shelf.Rev, &times[1], &actors[1], &sources[1],
		&kept, &s.Kept.Rev, &times[2], &actors[2], &sources[2], &s.StateRev)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("load entry state: %w", err)
	}
	s.Seen.Value = syncmodel.Value(seen)
	s.Shelf.Value = syncmodel.Value(shelf)
	if kept {
		s.Kept.Value = syncmodel.True
	}
	for i, r := range []*syncmodel.Register{&s.Seen, &s.Shelf, &s.Kept} {
		if times[i] != nil {
			r.OccurredAt = times[i].UTC()
		}
		if actors[i] != nil {
			r.Actor = syncmodel.ID(*actors[i])
		}
		if sources[i] != nil {
			r.Source = syncmodel.Source(*sources[i])
			if *sources[i] == "rule" || *sources[i] == "agent" {
				r.Source = syncmodel.SourceRuleOrAgent
			}
		}
	}
	return s, nil
}

// SaveState persists a state already resolved by syncmodel in the caller's transaction.
func SaveState(ctx context.Context, tx pgx.Tx, s syncmodel.State) error {
	stamp := func(r syncmodel.Register) (any, any, any) {
		if r.Rev == 0 {
			return nil, nil, nil
		}
		return r.OccurredAt, int64(r.Actor), string(r.Source)
	}
	st, sa, ss := stamp(s.Seen)
	ht, ha, hs := stamp(s.Shelf)
	kt, ka, ks := stamp(s.Kept)
	_, err := tx.Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,
 seen,seen_rev,seen_occurred_at,seen_actor,seen_source,
 shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,
 kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
 ON CONFLICT(principal_id,entry_id) DO UPDATE SET
 seen=EXCLUDED.seen,seen_rev=EXCLUDED.seen_rev,seen_occurred_at=EXCLUDED.seen_occurred_at,seen_actor=EXCLUDED.seen_actor,seen_source=EXCLUDED.seen_source,
 shelf=EXCLUDED.shelf,shelf_rev=EXCLUDED.shelf_rev,shelf_occurred_at=EXCLUDED.shelf_occurred_at,shelf_actor=EXCLUDED.shelf_actor,shelf_source=EXCLUDED.shelf_source,
 kept=EXCLUDED.kept,kept_rev=EXCLUDED.kept_rev,kept_occurred_at=EXCLUDED.kept_occurred_at,kept_actor=EXCLUDED.kept_actor,kept_source=EXCLUDED.kept_source,state_rev=EXCLUDED.state_rev`,
		int64(s.Principal), int64(s.EntryID), string(s.Seen.Value), int64(s.Seen.Rev), st, sa, ss,
		string(s.Shelf.Value), int64(s.Shelf.Rev), ht, ha, hs, s.Kept.Value == syncmodel.True, int64(s.Kept.Rev), kt, ka, ks, int64(s.StateRev))
	if err != nil {
		return fmt.Errorf("save resolved state: %w", err)
	}
	return nil
}
