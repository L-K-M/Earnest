package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Range is an inclusive allocated range. A zero Range means no allocation.
type Range struct{ First, Last int64 }

// Allocation contains the ingest sequence and each user's change revisions.
type Allocation struct {
	Seq  Range
	Revs map[int64]Range
}

// AllocateCounters locks seq first, then users in ascending ID order. Call it
// once, before taking other row locks in a mutation transaction. Allocations
// roll back with the mutation, so committed values are gapless. Callers must
// use every allocated number, and must not commit after an allocation error.
func AllocateCounters(ctx context.Context, tx pgx.Tx, seqCount int64, userCounts map[int64]int64) (Allocation, error) {
	if seqCount < 0 {
		return Allocation{}, fmt.Errorf("sequence count must be nonnegative")
	}
	ids := make([]int64, 0, len(userCounts))
	for id, count := range userCounts {
		if id <= 0 || count <= 0 {
			return Allocation{}, fmt.Errorf("user IDs and revision counts must be positive")
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var end int64
	if err := tx.QueryRow(ctx, "UPDATE seq_counter SET value=value+$1 WHERE singleton RETURNING value", seqCount).Scan(&end); err != nil {
		return Allocation{}, fmt.Errorf("allocate seq: %w", err)
	}
	out := Allocation{Revs: make(map[int64]Range, len(ids))}
	if seqCount > 0 {
		out.Seq = Range{First: end - seqCount + 1, Last: end}
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, "INSERT INTO rev_counters (user_id) VALUES ($1) ON CONFLICT DO NOTHING", id); err != nil {
			return Allocation{}, fmt.Errorf("ensure revision counter for user %d: %w", id, err)
		}
		if err := tx.QueryRow(ctx, "UPDATE rev_counters SET value=value+$2 WHERE user_id=$1 RETURNING value", id, userCounts[id]).Scan(&end); err != nil {
			return Allocation{}, fmt.Errorf("allocate revisions for user %d: %w", id, err)
		}
		out.Revs[id] = Range{First: end - userCounts[id] + 1, Last: end}
	}
	return out, nil
}
