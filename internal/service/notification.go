package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const NotificationDedupeWindow = 24 * time.Hour
const defaultNotificationBudget = 3

type NotificationCategory string

const (
	NotificationAlert  NotificationCategory = "alert"
	NotificationDigest NotificationCategory = "digest"
	NotificationBrief  NotificationCategory = "brief"
)

type NotificationStatus string

const (
	NotificationHeld       NotificationStatus = "held"
	NotificationQueued     NotificationStatus = "queued"
	NotificationAttempting NotificationStatus = "attempting"
	NotificationDelivered  NotificationStatus = "delivered"
	NotificationFailed     NotificationStatus = "failed"
	NotificationUncertain  NotificationStatus = "uncertain"
	NotificationCancelled  NotificationStatus = "cancelled"
	NotificationWithheld   NotificationStatus = "withheld"
)

type NotificationBudget struct {
	UsedToday  int `json:"used_today"`
	LimitToday int `json:"limit_today"`
}
type NotificationBudgetError struct{ Budget NotificationBudget }

func (e *NotificationBudgetError) Error() string {
	return fmt.Sprintf("notification budget %d/%d: %s", e.Budget.UsedToday, e.Budget.LimitToday, ErrRateLimited)
}
func (e *NotificationBudgetError) Unwrap() error { return ErrRateLimited }

type NotificationWhy struct {
	Sender         string             `json:"sender"`
	Budget         NotificationBudget `json:"budget"`
	MatchingRuleID int64              `json:"matching_rule_id,omitempty"`
}
type Notification struct {
	ID          int64                `json:"id"`
	PrincipalID int64                `json:"principal_id"`
	Note        string               `json:"note"`
	EntryIDs    []int64              `json:"entry_ids"`
	Category    NotificationCategory `json:"category"`
	Status      NotificationStatus   `json:"status"`
	HeldUntil   *time.Time           `json:"held_until,omitempty"`
	Budget      NotificationBudget   `json:"budget"`
	Why         NotificationWhy      `json:"why"`
	Generation  string               `json:"generation"`
	CreatedAt   time.Time            `json:"created_at"`
	Replay      bool                 `json:"replay"`
}
type SendNotificationRequest struct {
	OpID       string
	Generation string
	Note       string
	EntryIDs   []int64
	Category   NotificationCategory
	DedupeKey  string
}

func validNotificationCategory(c NotificationCategory) bool {
	return c == NotificationAlert || c == NotificationDigest || c == NotificationBrief
}

// SendNotification atomically accepts an operation, debits its token budget and records provenance.
func (s *Service) SendNotification(ctx context.Context, a Actor, r SendNotificationRequest) (Notification, error) {
	if err := s.checkActor(ctx, a, ScopeNotifySend); err != nil {
		return Notification{}, err
	}
	if !validOpID(r.OpID) || len(r.DedupeKey) > 256 || !validNotificationCategory(r.Category) {
		return Notification{}, ErrInvalid
	}
	note, err := plainText(r.Note, maxNoteLength)
	if err != nil {
		return Notification{}, err
	}
	ids, err := canonicalEntries(r.EntryIDs)
	if err != nil {
		return Notification{}, err
	}
	receipt := s.now().UTC()
	var out Notification
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		// This must precede op-id lookup: a restore can forget acceptance entirely.
		if r.Generation != current.Generation {
			return ErrGone
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "SendNotification", r.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			if err := json.Unmarshal(replay, &out); err != nil {
				return err
			}
			out.Replay = true
			return nil
		}
		if err := visibleEntries(ctx, tx, a.UserID, ids); err != nil {
			return err
		}
		if r.DedupeKey != "" {
			var id int64
			err := tx.QueryRow(ctx, "SELECT id FROM notifications WHERE sender_principal_id=$1 AND dedupe_key=$2 AND created_at>$3 AND generation=$4 ORDER BY id DESC LIMIT 1", a.PrincipalID, r.DedupeKey, receipt.Add(-NotificationDedupeWindow), r.Generation).Scan(&id)
			if err == nil {
				out, err = loadNotification(ctx, tx, a.UserID, id)
				if err != nil {
					return err
				}
				out.Replay = true
				return store.FinishOp(ctx, tx, a.PrincipalID, r.OpID, out)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		settings, err := s.pushSettingsTx(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return fmt.Errorf("push timezone: %w", err)
		}
		local := receipt.In(loc)
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		end := start.AddDate(0, 0, 1)
		budget := NotificationBudget{LimitToday: defaultNotificationBudget}
		if a.TokenID != 0 {
			err := tx.QueryRow(ctx, "SELECT daily_budget FROM tokens WHERE id=$1 AND principal_id=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>$3)", a.TokenID, a.PrincipalID, receipt).Scan(&budget.LimitToday)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnauthorized
			}
			if err != nil {
				return err
			}
			err = tx.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE token_id=$1 AND created_at>=$2 AND created_at<$3", a.TokenID, start, end).Scan(&budget.UsedToday)
			if err != nil {
				return err
			}
		} else {
			if a.Kind == PrincipalAgent {
				return ErrUnauthorized
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE sender_principal_id=$1 AND token_id IS NULL AND created_at>=$2 AND created_at<$3", a.PrincipalID, start, end).Scan(&budget.UsedToday); err != nil {
				return err
			}
		}
		if budget.UsedToday >= budget.LimitToday {
			return &NotificationBudgetError{Budget: budget}
		}
		budget.UsedToday++
		var sender string
		if err := tx.QueryRow(ctx, "SELECT name FROM principals WHERE id=$1", a.PrincipalID).Scan(&sender); err != nil {
			return err
		}
		out = Notification{PrincipalID: a.PrincipalID, Note: note, EntryIDs: ids, Category: r.Category, Status: NotificationQueued, Budget: budget, Why: NotificationWhy{Sender: sender, Budget: budget}, Generation: r.Generation, CreatedAt: receipt}
		var muted bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notification_mutes WHERE user_id=$1 AND principal_id=$2)", a.UserID, a.PrincipalID).Scan(&muted); err != nil {
			return err
		}
		until := quietUntil(settings, receipt)
		if muted {
			until = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
		} else if a.Has(ScopeNotifyUrgent) {
			until = time.Time{}
		}
		if !until.IsZero() {
			out.Status = NotificationHeld
			out.HeldUntil = &until
		}
		why, err := json.Marshal(out.Why)
		if err != nil {
			return err
		}
		var token any
		if a.TokenID != 0 {
			token = a.TokenID
		}
		err = tx.QueryRow(ctx, `INSERT INTO notifications(sender_principal_id,recipient_user_id,op_id,token_id,note,entry_ids,category,dedupe_key,status,held_until,generation,why,budget_used,budget_limit,created_at,urgent)
 VALUES($1,$2,$3,$4,$5,$6,$7,nullif($8,''),$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`, a.PrincipalID, a.UserID, r.OpID, token, note, ids, r.Category, r.DedupeKey, out.Status, out.HeldUntil, r.Generation, why, budget.UsedToday, budget.LimitToday, out.CreatedAt, a.Has(ScopeNotifyUrgent)).Scan(&out.ID)
		if err != nil {
			return fmt.Errorf("accept notification: %w", err)
		}
		if err := s.recordActivity(ctx, tx, a, "notification.created", map[string]any{"id": out.ID}, nil, out); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, r.OpID, out)
	})
	return out, domainError(err)
}

type notificationReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadNotification(ctx context.Context, q notificationReader, user, id int64) (Notification, error) {
	var out Notification
	var why []byte
	err := q.QueryRow(ctx, "SELECT id,sender_principal_id,note,entry_ids,category,status,held_until,budget_used,budget_limit,why,generation::text,created_at FROM notifications WHERE recipient_user_id=$1 AND id=$2", user, id).Scan(&out.ID, &out.PrincipalID, &out.Note, &out.EntryIDs, &out.Category, &out.Status, &out.HeldUntil, &out.Budget.UsedToday, &out.Budget.LimitToday, &why, &out.Generation, &out.CreatedAt)
	if err != nil {
		return out, domainError(err)
	}
	if err := json.Unmarshal(why, &out.Why); err != nil {
		return out, err
	}
	return out, nil
}
func (s *Service) notificationHuman(ctx context.Context, a Actor) error {
	if a.Kind != PrincipalHuman {
		return ErrForbidden
	}
	return s.checkActor(ctx, a, ScopeEntriesRead)
}

// GetNotification exposes stored agent words only to the human.
func (s *Service) GetNotification(ctx context.Context, a Actor, id int64) (Notification, error) {
	if err := s.notificationHuman(ctx, a); err != nil {
		return Notification{}, err
	}
	return loadNotification(ctx, s.store.Pool(), a.UserID, id)
}
func (s *Service) ListNotifications(ctx context.Context, a Actor, beforeID int64, limit int) ([]Notification, error) {
	if err := s.notificationHuman(ctx, a); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 || beforeID < 0 {
		return nil, ErrInvalid
	}
	rows, err := s.store.Pool().Query(ctx, "SELECT id FROM notifications WHERE recipient_user_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3", a.UserID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Notification{}
	for _, id := range ids {
		n, err := loadNotification(ctx, s.store.Pool(), a.UserID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// CancelNotification permits the human or its author to cancel only while held.
func (s *Service) CancelNotification(ctx context.Context, a Actor, id int64, opID string) error {
	if err := s.checkActor(ctx, a, ScopeNotifySend); err != nil {
		return err
	}
	if !validOpID(opID) {
		return ErrInvalid
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "CancelNotification", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		n, err := loadNotification(ctx, tx, a.UserID, id)
		if err != nil {
			return err
		}
		if a.Kind != PrincipalHuman && n.PrincipalID != a.PrincipalID {
			return ErrForbidden
		}
		if n.Status != NotificationHeld {
			return fmt.Errorf("notification is %s: %w", n.Status, ErrConflict)
		}
		if _, err := tx.Exec(ctx, "UPDATE notifications SET status='cancelled' WHERE id=$1", id); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, tx, a, "notification.cancelled", map[string]any{"id": id}, n, map[string]any{"status": "cancelled"}); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"id": id})
	})
}

type NotificationMute string

const (
	NotificationsMuted   NotificationMute = "muted"
	NotificationsUnmuted NotificationMute = "unmuted"
)

// MuteAgent changes only delivery policy. Accepted muted notifications still consume budget.
func (s *Service) MuteAgent(ctx context.Context, a Actor, principal int64, mode NotificationMute, opID string) error {
	if a.Kind != PrincipalHuman {
		return ErrForbidden
	}
	if err := s.checkActor(ctx, a, ScopeDevicesWrite); err != nil {
		return err
	}
	if !validOpID(opID) || (mode != NotificationsMuted && mode != NotificationsUnmuted) {
		return ErrInvalid
	}
	muted := mode == NotificationsMuted
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "MuteAgent", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM principals WHERE id=$1 AND user_id=$2 AND kind='agent')", principal, a.UserID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if muted {
			_, err = tx.Exec(ctx, "INSERT INTO notification_mutes(user_id,principal_id) VALUES($1,$2) ON CONFLICT DO NOTHING", a.UserID, principal)
		} else {
			_, err = tx.Exec(ctx, "DELETE FROM notification_mutes WHERE user_id=$1 AND principal_id=$2", a.UserID, principal)
		}
		if err != nil {
			return err
		}
		if !muted {
			if _, err := tx.Exec(ctx, "UPDATE notifications SET held_until=$3 WHERE recipient_user_id=$1 AND sender_principal_id=$2 AND status='held'", a.UserID, principal, s.now()); err != nil {
				return err
			}
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"principal": principal, "muted": muted})
	})
}

// ReleaseNotifications opens the current generation only. Old pending rows remain withheld.
func (s *Service) ReleaseNotifications(ctx context.Context, a Actor, generation, opID string) error {
	if a.Kind != PrincipalHuman {
		return ErrForbidden
	}
	if err := s.checkActor(ctx, a, ScopeDevicesWrite); err != nil {
		return err
	}
	if !validOpID(opID) {
		return ErrInvalid
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if generation != current.Generation {
			return ErrGone
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "ReleaseNotifications", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		if _, err := tx.Exec(ctx, "UPDATE system SET notifications_released=true WHERE singleton"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE notifications SET status=CASE WHEN held_until>$3 THEN 'held' ELSE 'queued' END WHERE recipient_user_id=$1 AND generation=$2 AND status='withheld' AND attempted_at IS NULL", a.UserID, generation, s.now()); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"generation": generation})
	})
}
