package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// broker coalesces hints: the durable change log remains the source of truth.
type broker struct {
	mu        sync.Mutex
	listeners map[int64]map[chan struct{}]struct{}
}

func (b *broker) subscribe(user int64) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.listeners == nil {
		b.listeners = make(map[int64]map[chan struct{}]struct{})
	}
	if b.listeners[user] == nil {
		b.listeners[user] = make(map[chan struct{}]struct{})
	}
	b.listeners[user][ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.listeners[user], ch)
			if len(b.listeners[user]) == 0 {
				delete(b.listeners, user)
			}
		})
	}
}

func (b *broker) publish(user int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.listeners[user] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// WatchChanges subscribes to committed changes for this user's full feed.
// Cancel releases the subscription; channels are never closed by publishers.
func (s *Service) WatchChanges(ctx context.Context, actor Actor) (<-chan struct{}, func(), error) {
	if err := s.checkActor(ctx, actor, ScopeEntriesRead); err != nil {
		return nil, nil, err
	}
	ch, cancel := s.events.subscribe(actor.UserID)
	return ch, cancel, nil
}

type eventTx struct {
	pgx.Tx
	users map[int64]struct{}
}

func insertEvent(ctx context.Context, tx pgx.Tx, user, rev int64, kind string, payload any, at time.Time) error {
	if err := store.InsertChange(ctx, tx, user, rev, kind, payload, at); err != nil {
		return err
	}
	if tracked, ok := tx.(*eventTx); ok {
		tracked.users[user] = struct{}{}
	}
	return nil
}

// insertEvents writes one event per (rev, payload) pair in a single statement.
func insertEvents(ctx context.Context, tx pgx.Tx, user int64, revs []int64, kind string, payloads []json.RawMessage, at time.Time) error {
	if err := store.InsertChanges(ctx, tx, user, revs, kind, payloads, at); err != nil {
		return err
	}
	if tracked, ok := tx.(*eventTx); ok && len(revs) > 0 {
		tracked.users[user] = struct{}{}
	}
	return nil
}

func (s *Service) withEvents(ctx context.Context, fn func(pgx.Tx) error) error {
	tracked := &eventTx{users: make(map[int64]struct{})}
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		tracked.Tx = tx
		return fn(tracked)
	})
	if err != nil {
		return err
	}
	// WithTx has committed successfully before any subscriber can see a hint.
	for user := range tracked.users {
		s.events.publish(user)
	}
	return nil
}

// WatchChanges subscribes to authentication changes, including token revocation.
func (a *Auth) WatchChanges(ctx context.Context, actor Actor) (<-chan struct{}, func(), error) {
	if actor.UserID <= 0 || !actor.Has(ScopeEntriesRead) {
		return nil, nil, ErrForbidden
	}
	ch, cancel := a.events.subscribe(actor.UserID)
	return ch, cancel, nil
}

// RefreshSubscription schedules a human's feed through the leased fetcher.
// It never clears a live lease or performs network I/O in the transaction.
func (s *Service) RefreshSubscription(ctx context.Context, actor Actor, id int64, opID string) error {
	if err := s.checkActor(ctx, actor, ScopeFeedsWrite); err != nil {
		return err
	}
	if id <= 0 || !validOpID(opID) {
		return ErrInvalid
	}
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		previous, err := replayOp(ctx, tx, actor.PrincipalID, "RefreshSubscription", opID)
		if err != nil {
			return err
		}
		if previous != nil {
			return nil
		}
		subscription, err := subscriptionTx(ctx, tx, actor.UserID, id)
		if err != nil {
			return err
		}
		if subscription.Deleted {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, "UPDATE feeds SET next_fetch_at=LEAST(next_fetch_at,$2) WHERE id=$1", subscription.FeedID, s.now().UTC()); err != nil {
			return fmt.Errorf("schedule feed refresh: %w", err)
		}
		return store.FinishOp(ctx, tx, actor.PrincipalID, opID, struct{}{})
	})
	return domainError(err)
}
