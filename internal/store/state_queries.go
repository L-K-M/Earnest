package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// SaveStates persists resolved states with one upsert, equivalent to calling
// SaveState on each in order. (principal_id, entry_id) pairs must be distinct:
// a repeated target is an error here where sequential statements would
// overwrite the row.
func SaveStates(ctx context.Context, tx pgx.Tx, states []syncmodel.State) error {
	if len(states) == 0 {
		return nil
	}
	n := len(states)
	principals := make([]int64, n)
	entries := make([]int64, n)
	seen := make([]string, n)
	seenRev := make([]int64, n)
	seenAt := make([]pgtype.Timestamptz, n)
	seenActor := make([]pgtype.Int8, n)
	seenSource := make([]pgtype.Text, n)
	shelf := make([]string, n)
	shelfRev := make([]int64, n)
	shelfAt := make([]pgtype.Timestamptz, n)
	shelfActor := make([]pgtype.Int8, n)
	shelfSource := make([]pgtype.Text, n)
	kept := make([]bool, n)
	keptRev := make([]int64, n)
	keptAt := make([]pgtype.Timestamptz, n)
	keptActor := make([]pgtype.Int8, n)
	keptSource := make([]pgtype.Text, n)
	stateRev := make([]int64, n)
	stamp := func(r syncmodel.Register) (pgtype.Timestamptz, pgtype.Int8, pgtype.Text) {
		if r.Rev == 0 {
			return pgtype.Timestamptz{}, pgtype.Int8{}, pgtype.Text{}
		}
		return pgtype.Timestamptz{Time: r.OccurredAt, Valid: true},
			pgtype.Int8{Int64: int64(r.Actor), Valid: true},
			pgtype.Text{String: string(r.Source), Valid: true}
	}
	for i, s := range states {
		principals[i], entries[i] = int64(s.Principal), int64(s.EntryID)
		seen[i], seenRev[i] = string(s.Seen.Value), int64(s.Seen.Rev)
		seenAt[i], seenActor[i], seenSource[i] = stamp(s.Seen)
		shelf[i], shelfRev[i] = string(s.Shelf.Value), int64(s.Shelf.Rev)
		shelfAt[i], shelfActor[i], shelfSource[i] = stamp(s.Shelf)
		kept[i], keptRev[i] = s.Kept.Value == syncmodel.True, int64(s.Kept.Rev)
		keptAt[i], keptActor[i], keptSource[i] = stamp(s.Kept)
		stateRev[i] = int64(s.StateRev)
	}
	_, err := tx.Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,
seen,seen_rev,seen_occurred_at,seen_actor,seen_source,
shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,
kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev)
SELECT * FROM unnest($1::bigint[],$2::bigint[],$3::text[],$4::bigint[],$5::timestamptz[],$6::bigint[],$7::text[],
$8::text[],$9::bigint[],$10::timestamptz[],$11::bigint[],$12::text[],
$13::boolean[],$14::bigint[],$15::timestamptz[],$16::bigint[],$17::text[],$18::bigint[])
ON CONFLICT(principal_id,entry_id) DO UPDATE SET
seen=EXCLUDED.seen,seen_rev=EXCLUDED.seen_rev,seen_occurred_at=EXCLUDED.seen_occurred_at,seen_actor=EXCLUDED.seen_actor,seen_source=EXCLUDED.seen_source,
shelf=EXCLUDED.shelf,shelf_rev=EXCLUDED.shelf_rev,shelf_occurred_at=EXCLUDED.shelf_occurred_at,shelf_actor=EXCLUDED.shelf_actor,shelf_source=EXCLUDED.shelf_source,
kept=EXCLUDED.kept,kept_rev=EXCLUDED.kept_rev,kept_occurred_at=EXCLUDED.kept_occurred_at,kept_actor=EXCLUDED.kept_actor,kept_source=EXCLUDED.kept_source,state_rev=EXCLUDED.state_rev`,
		principals, entries, seen, seenRev, seenAt, seenActor, seenSource,
		shelf, shelfRev, shelfAt, shelfActor, shelfSource,
		kept, keptRev, keptAt, keptActor, keptSource, stateRev)
	if err != nil {
		return fmt.Errorf("save resolved states: %w", err)
	}
	return nil
}
