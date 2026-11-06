package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const maxPushTextRunes = 512

// maxClaimCandidates bounds the rows one claim inspects, the bound the
// former locked shortlist applied.
const maxClaimCandidates = 100

type notificationAttempt struct {
	notification Notification
	user         int64
	devices      []push.Device
}

// DeliverNotifications claims at most limit rows. A committed attempt is never reclaimed.
func (s *Service) DeliverNotifications(ctx context.Context, limit int) (int, error) {
	if s.pushRegistry == nil {
		return 0, ErrUnavailable
	}
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	count := 0
	for range limit {
		attempt, err := s.claimNotification(ctx)
		if err != nil {
			return count, err
		}
		if attempt == nil {
			return count, nil
		}
		outcome := push.OutcomeNotDelivered
		for _, device := range attempt.devices {
			transport, err := s.pushRegistry.Get(device.Kind)
			if err == nil {
				message := push.Message{Title: "Earnest", Body: fmt.Sprintf("Agent notification: 1 %s", attempt.notification.Category), NotificationID: strconv.FormatInt(attempt.notification.ID, 10), Category: push.Category(attempt.notification.Category)}
				if device.TextAllowed() {
					// A preview fits the encrypted Web Push record even with JSON escaping.
					text := []rune(attempt.notification.Note)
					message.AgentText = string(text[:min(len(text), maxPushTextRunes)])
				}
				_, err = transport.Send(ctx, device, message)
			} else {
				err = &push.Error{Kind: device.Kind, Outcome: push.OutcomeNotDelivered, Err: err}
			}
			result := push.OutcomeOf(err)
			// One acceptance proves delivery. Failure means every device
			// definitely rejected it; uncertainty never justifies a retry.
			if result == push.OutcomeDelivered {
				outcome = push.OutcomeDelivered
			} else if result == push.OutcomeUnknown && outcome != push.OutcomeDelivered {
				outcome = push.OutcomeUnknown
			}
		}
		if err := s.finishNotification(ctx, *attempt, outcome); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
func (s *Service) claimNotification(ctx context.Context) (*notificationAttempt, error) {
	var attempt *notificationAttempt
	// A worker may skip muted or newly quiet rows and continue to the next
	// candidate. Rows held for quiet hours commit in their own transaction
	// and are excluded by id, so one claim never locks more than one row.
	held := []int64{}
	for len(held) < maxClaimCandidates && attempt == nil {
		inspected := false
		err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
			if err := store.LockCore(ctx, tx); err != nil {
				return err
			}
			var generation string
			var released bool
			if err := tx.QueryRow(ctx, "SELECT generation::text,notifications_released FROM system WHERE singleton").Scan(&generation, &released); err != nil {
				return err
			}
			if !released {
				return nil
			}
			var id, user int64
			var urgent bool
			err := tx.QueryRow(ctx, `SELECT n.id,n.recipient_user_id,n.urgent FROM notifications n
 WHERE n.generation=$1 AND n.attempted_at IS NULL AND (n.status='queued' OR (n.status='held' AND n.held_until<=$2))
 AND NOT EXISTS(SELECT 1 FROM notification_mutes m WHERE m.user_id=n.recipient_user_id AND m.principal_id=n.sender_principal_id)
 AND NOT (n.id = ANY($3::bigint[]))
 ORDER BY n.id FOR UPDATE OF n SKIP LOCKED LIMIT 1`, generation, s.now(), held).Scan(&id, &user, &urgent)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			inspected = true
			n, err := loadNotification(ctx, tx, user, id)
			if err != nil {
				return err
			}
			settings, err := s.pushSettingsTx(ctx, tx, user)
			if err != nil {
				return err
			}
			if until := quietUntil(settings, s.now()); !urgent && !until.IsZero() {
				if _, err := tx.Exec(ctx, "UPDATE notifications SET status='held',held_until=$2 WHERE id=$1", id, until); err != nil {
					return err
				}
				held = append(held, id)
				return nil
			}
			devices, err := s.notificationDevicesFor(ctx, tx, user, settings)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE notifications SET status='attempting',attempted_at=$2 WHERE id=$1", id, s.now()); err != nil {
				return err
			}
			attempt = &notificationAttempt{notification: n, user: user, devices: devices}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !inspected {
			break
		}
	}
	return attempt, nil
}
func (s *Service) finishNotification(ctx context.Context, a notificationAttempt, outcome push.Outcome) error {
	status := NotificationUncertain
	switch outcome {
	case push.OutcomeDelivered:
		status = NotificationDelivered
	case push.OutcomeNotDelivered:
		status = NotificationFailed
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		var generation string
		if err := tx.QueryRow(ctx, "SELECT generation::text FROM system WHERE singleton").Scan(&generation); err != nil {
			return err
		}
		if generation != a.notification.Generation {
			return ErrGone
		}
		var delivered *time.Time
		if status == NotificationDelivered {
			now := s.now()
			delivered = &now
		}
		_, err := tx.Exec(ctx, "UPDATE notifications SET status=$2,delivered_at=$3 WHERE id=$1 AND status='attempting' AND generation=$4", a.notification.ID, status, delivered, generation)
		if err != nil {
			return fmt.Errorf("finish notification attempt: %w", err)
		}
		return nil
	})
}

// RecoverNotificationAttempts marks interrupted sends uncertain at startup.
// Call with all delivery workers stopped; none of these rows will be retried.
func (s *Service) RecoverNotificationAttempts(ctx context.Context) (int64, error) {
	var count int64
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE notifications SET status='uncertain' WHERE status='attempting' AND attempted_at IS NOT NULL")
		if err != nil {
			return fmt.Errorf("recover notification attempts: %w", err)
		}
		count = tag.RowsAffected()
		return nil
	})
	return count, err
}
