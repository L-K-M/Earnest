package service

import (
	"context"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const (
	// defaultRetention bounds the change log and entry bodies when a job does
	// not override it.
	defaultRetention = 90 * 24 * time.Hour
	// opIDRetention keeps operation deduplication records longer than the
	// maximum offline queue plus retry margin, independent of log retention.
	// Pruning them early would let a retried write apply twice.
	opIDRetention = 90 * 24 * time.Hour
)

func retentionOrDefault(retention time.Duration) (time.Duration, error) {
	if retention < 0 {
		return 0, fmt.Errorf("retention: %w", ErrInvalid)
	}
	if retention == 0 {
		return defaultRetention, nil
	}
	return retention, nil
}

// RetainChanges deletes the contiguous old prefix of every user's change log
// older than retention and advances each user's retention floor to the
// highest revision removed, so a cursor exactly at the floor still replays.
// The floor never regresses, and a clock moved backwards cannot open a hole:
// revisions newer than a retained row stay even when they are old enough.
// Operation deduplication records are retained separately for the full queue
// age plus margin; they are never pruned on the log's shorter schedule.
func (s *Service) RetainChanges(ctx context.Context, now time.Time, retention time.Duration) (int64, error) {
	retention, err := retentionOrDefault(retention)
	if err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = s.now()
	}
	var deleted int64
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		floors, err := store.DeleteOldChanges(ctx, tx, now.Add(-retention))
		if err != nil {
			return err
		}
		for _, floor := range floors {
			if err := store.AdvanceChangeFloor(ctx, tx, floor.UserID, floor.Rev); err != nil {
				return err
			}
			deleted += floor.Deleted
		}
		return store.DeleteOldOpIDs(ctx, tx, now.Add(-opIDRetention))
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// RetainEntries tombstones entries older than retention once every active
// subscriber's threshold permits: a user settings retention_days overrides the
// job's default. Entries a principal kept or shelved as Later survive, as do
// already tombstoned rows; only first_seen age decides.
//
// A tombstone keeps the entry's identity so ingestion can never resurrect it,
// deletes its stored bodies, and emits one gapless entry.deleted per current
// subscriber together with the entry_revisions stamp. Allocation is exact:
// the counters move once, after the lock and subscriber discovery.
func (s *Service) RetainEntries(ctx context.Context, now time.Time, retention time.Duration) (int, error) {
	retention, err := retentionOrDefault(retention)
	if err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = s.now()
	}
	var count int
	err = s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		candidates, err := store.DiscoverRetentionEntries(ctx, tx, now, retention)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(candidates))
		for _, e := range candidates {
			ids = append(ids, e.ID)
		}
		subscribers, err := store.SubscribersForEntries(ctx, tx, ids)
		if err != nil {
			return err
		}
		counts := map[int64]int64{}
		for _, e := range candidates {
			for _, sub := range subscribers[e.ID] {
				counts[sub.UserID]++
			}
		}
		allocation, err := store.AllocateCounters(ctx, tx, 0, counts)
		if err != nil {
			return err
		}
		next := map[int64]int64{}
		for user, r := range allocation.Revs {
			next[user] = r.First
		}
		for _, e := range candidates {
			for _, sub := range subscribers[e.ID] {
				rev := next[sub.UserID]
				next[sub.UserID]++
				entry := syncmodel.Entry{
					ID:             syncmodel.ID(e.ID),
					SubscriptionID: syncmodel.ID(sub.SubscriptionID),
					Seq:            uint64(e.Seq),
					FirstSeenAt:    e.FirstSeenAt.UTC(),
					Rev:            uint64(rev),
					Deleted:        true,
				}
				if e.PublishedAt != nil {
					entry.PublishedAt = e.PublishedAt.UTC()
				}
				if err := s.emitChange(ctx, tx, sub.UserID, rev, syncmodel.EntryDeleted, "entry", entry); err != nil {
					return err
				}
				if err := store.StampEntryRevision(ctx, tx, sub.UserID, e.ID, rev); err != nil {
					return err
				}
			}
			if err := store.TombstoneEntry(ctx, tx, e.ID, now.UTC()); err != nil {
				return err
			}
			if err := store.DeleteContents(ctx, tx, e.ID); err != nil {
				return err
			}
		}
		count = len(candidates)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
