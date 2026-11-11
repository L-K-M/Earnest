package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	webhookJobKind     = "webhook"
	webhookJobLease    = 5 * time.Minute
	webhookSendTimeout = 15 * time.Second
	webhookResponseMax = 64 << 10
	webhookMaxClaim    = 1000
	// webhookMaxFailures disables a destination after this many consecutive
	// definite delivery failures.
	webhookMaxFailures = 5
	webhookBaseBackoff = 30 * time.Second
	webhookMaxBackoff  = time.Hour
)

// webhookJob is the durable jobs-table payload: enough to sign and send, bound
// to the database generation so a restore can never replay a send.
type webhookJob struct {
	WebhookID  int64  `json:"webhook_id"`
	MessageID  string `json:"message_id"`
	Generation string `json:"generation"`
	Event      string `json:"event"`
	Body       string `json:"body"`
}

// emitWebhooks enqueues one durable job per active webhook of the user that
// subscribes to the event type. It must run inside the transaction that
// produces the event so the mutation and its jobs commit atomically.
func (s *Service) emitWebhooks(ctx context.Context, tx pgx.Tx, user int64, ev WebhookEvent) error {
	if user <= 0 {
		return fmt.Errorf("webhook event user: %w", ErrInvalid)
	}
	if err := ev.validate(); err != nil {
		return err
	}
	body, err := ev.body(s.now())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs (kind,payload,unique_key,run_at)
  SELECT $1, jsonb_build_object(
    'generation',(SELECT generation::text FROM system WHERE singleton),
    'webhook_id',w.id,
    'message_id','msg_'||replace(gen_random_uuid()::text,'-',''),
    'event',$2::text,
    'body',$3::text),
    'wh:'||w.id||':'||$4, $6
  FROM webhooks w WHERE w.user_id=$5 AND w.active AND $2=ANY(w.events)
  ON CONFLICT (unique_key) DO NOTHING`,
		webhookJobKind, string(ev.Type), body, ev.dedupeKey(), user, s.now().UTC())
	if err != nil {
		return fmt.Errorf("enqueue webhook jobs: %w", err)
	}
	return nil
}

// EmitWebhookEvent enqueues deliveries for one user's webhooks. Callers inside
// a mutation transaction use emitWebhooks so the jobs commit with the mutation.
func (s *Service) EmitWebhookEvent(ctx context.Context, user int64, ev WebhookEvent) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		return s.emitWebhooks(ctx, tx, user, ev)
	})
}

// emitFeedFailing enqueues feed.failing for every user subscribed to the feed
// who owns an active webhook listening for it.
func (s *Service) emitFeedFailing(ctx context.Context, tx pgx.Tx, feedID int64) error {
	ev := WebhookEvent{Type: WebhookFeedFailing, FeedID: feedID}
	if err := ev.validate(); err != nil {
		return err
	}
	body, err := ev.body(s.now())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs (kind,payload,unique_key,run_at)
  SELECT $1, jsonb_build_object(
    'generation',(SELECT generation::text FROM system WHERE singleton),
    'webhook_id',w.id,
    'message_id','msg_'||replace(gen_random_uuid()::text,'-',''),
    'event',$2::text,
    'body',$3::text),
    'wh:'||w.id||':'||$4, $6
  FROM webhooks w WHERE w.active AND $2=ANY(w.events)
  AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=$5 AND s.user_id=w.user_id AND s.deleted_at IS NULL)
  ON CONFLICT (unique_key) DO NOTHING`,
		webhookJobKind, string(ev.Type), body, ev.dedupeKey(), feedID, s.now().UTC())
	if err != nil {
		return fmt.Errorf("enqueue webhook jobs: %w", err)
	}
	return nil
}

// EmitFeedFailing reports a failing feed to its subscribers' webhooks.
func (s *Service) EmitFeedFailing(ctx context.Context, feedID int64) error {
	if feedID <= 0 {
		return fmt.Errorf("feed id: %w", ErrInvalid)
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		return s.emitFeedFailing(ctx, tx, feedID)
	})
}

// claimedWebhookJob is a durable send attempt committed before any network use.
type claimedWebhookJob struct {
	jobID      int64
	webhookID  int64
	attempts   int
	messageID  string
	generation string
	body       []byte
	url        string
	sealed     []byte
}

// RunWebhookJobs delivers due webhook jobs until the limit or the queue runs
// out, returning how many due jobs were processed (sent, rescheduled or
// dropped). Each job is claimed with SKIP LOCKED and its attempt committed
// before the request is made, so a crashed attempt is never repeated.
func (s *Service) RunWebhookJobs(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > webhookMaxClaim {
		return 0, ErrInvalid
	}
	if s.outbound == nil {
		return 0, fmt.Errorf("outbound client: %w", ErrUnavailable)
	}
	processed := 0
	for processed < limit {
		job, due, err := s.claimWebhookJob(ctx)
		if err != nil {
			return processed, err
		}
		if !due {
			break
		}
		processed++
		if job == nil {
			continue
		}
		if err := s.deliverWebhookJob(ctx, job); err != nil {
			return processed, err
		}
	}
	return processed, nil
}

// claimWebhookJob takes the next due webhook job and commits the attempt, or
// deletes the job durably when it must never send: an unknown prior outcome, a
// stale generation, or a destination that is gone, inactive or unsubscribed.
func (s *Service) claimWebhookJob(ctx context.Context) (*claimedWebhookJob, bool, error) {
	var job *claimedWebhookJob
	var due bool
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var jobID, attempts int64
		var payload []byte
		var lastError *string
		var webhookID *int64
		var url *string
		var sealed []byte
		var active *bool
		var events []string
		var generation string
		err := tx.QueryRow(ctx, `SELECT j.id,j.payload,j.attempts,j.last_error,
    w.id,w.url,w.secret_encrypted,w.active,w.events,
    (SELECT generation::text FROM system WHERE singleton)
  FROM jobs j LEFT JOIN webhooks w ON w.id = (j.payload->>'webhook_id')::bigint
  WHERE j.kind=$1 AND j.run_at<=$2 AND (j.locked_until IS NULL OR j.locked_until<=$2)
  ORDER BY j.run_at, j.id
  FOR UPDATE OF j SKIP LOCKED
  LIMIT 1`, webhookJobKind, s.now().UTC()).Scan(
			&jobID, &payload, &attempts, &lastError,
			&webhookID, &url, &sealed, &active, &events, &generation)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim webhook job: %w", err)
		}
		due = true
		var body webhookJob
		if err := json.Unmarshal(payload, &body); err != nil {
			return fmt.Errorf("decode webhook job: %w", err)
		}
		drop := body.Generation != generation ||
			(attempts > 0 && lastError == nil) ||
			webhookID == nil || !*active || !slices.Contains(events, body.Event)
		if drop {
			if _, err := tx.Exec(ctx, "DELETE FROM jobs WHERE id=$1", jobID); err != nil {
				return fmt.Errorf("drop webhook job: %w", err)
			}
			return nil
		}
		job = &claimedWebhookJob{jobID: jobID, webhookID: *webhookID, attempts: int(attempts) + 1,
			generation: body.Generation, messageID: body.MessageID, body: []byte(body.Body), url: *url, sealed: sealed}
		if _, err := tx.Exec(ctx, "UPDATE jobs SET attempts=attempts+1,last_error=NULL,locked_until=$2 WHERE id=$1",
			jobID, s.now().UTC().Add(webhookJobLease)); err != nil {
			return fmt.Errorf("record webhook attempt: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return job, due, nil
}

type webhookOutcome int

const (
	outcomeDelivered webhookOutcome = iota
	outcomeDefinite                 // not delivered for certain; safe to retry
	outcomeAmbiguous                // unknown outcome; never retried
)

type webhookResult struct {
	outcome    webhookOutcome
	class      string
	retryAfter time.Duration
}

// classifyWebhook maps the outbound result onto the delivery outcomes. A
// definite failure can never have reached the receiver or was plainly refused,
// so retrying cannot double-deliver; anything else is ambiguous.
func classifyWebhook(result *outbound.Result, err error) webhookResult {
	if err == nil {
		code := result.Response.StatusCode
		if code >= 200 && code <= 299 {
			return webhookResult{outcome: outcomeDelivered}
		}
		// Not following a redirect does not prove that the POST was refused.
		redirect := code >= http.StatusMultipleChoices && code < http.StatusBadRequest
		if code == http.StatusRequestTimeout || redirect {
			return webhookResult{outcome: outcomeAmbiguous, class: fmt.Sprintf("http_%d", code)}
		}
		return webhookResult{outcome: outcomeDefinite, class: fmt.Sprintf("http_%d", code)}
	}
	var oe *outbound.Error
	var retryAfter time.Duration
	if errors.As(err, &oe) {
		retryAfter = oe.RetryAfter
	}
	class := outbound.ErrorClass(err)
	// Connect includes read/write failures too. Only a failed dial proves
	// that this POST never reached the receiver.
	var network *net.OpError
	failedDial := class == outbound.Connect && errors.As(err, &network) && network.Op == "dial"
	if webhookDefinite(class) || failedDial {
		return webhookResult{outcome: outcomeDefinite, class: string(class), retryAfter: retryAfter}
	}
	return webhookResult{outcome: outcomeAmbiguous, class: string(class), retryAfter: retryAfter}
}

// webhookDefinite reports the outbound classes that prove the request was
// refused before processing, making a retry safe. Timeouts, 5xx, unreadable
// responses and unclassified errors may have been processed and stay ambiguous.
func webhookDefinite(class outbound.Class) bool {
	switch class {
	case outbound.DeniedAddress, outbound.DNS, outbound.TLS,
		outbound.RedirectLoop, outbound.HTTPGone, outbound.HTTP403, outbound.HTTP429:
		return true
	}
	return false
}

func webhookBackoff(attempts int, retryAfter time.Duration) time.Duration {
	exponent := min(max(attempts-1, 0), 8)
	return min(max(webhookBaseBackoff<<exponent, retryAfter), webhookMaxBackoff)
}

// deliverWebhookJob signs and sends one claimed job, then resolves the outcome.
func (s *Service) deliverWebhookJob(ctx context.Context, job *claimedWebhookJob) error {
	secret, err := s.openWebhookSecret(job.sealed)
	var result webhookResult
	if err != nil {
		result = webhookResult{outcome: outcomeDefinite, class: "envelope"}
	} else {
		result = s.sendWebhookJob(ctx, job, secret)
	}
	return s.resolveWebhookJob(ctx, job.jobID, job.webhookID, job.attempts, result, job.generation)
}

func (s *Service) sendWebhookJob(ctx context.Context, job *claimedWebhookJob, secret string) webhookResult {
	timestamp := s.now().Unix()
	signature, err := signWebhook(secret, job.messageID, timestamp, job.body)
	if err != nil {
		return webhookResult{outcome: outcomeDefinite, class: "envelope"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.url, bytes.NewReader(job.body))
	if err != nil {
		return webhookResult{outcome: outcomeDefinite, class: "request"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", job.messageID)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("webhook-signature", signature)
	// The subscriber chose this destination, so its own port is permitted;
	// the outbound address policy still rejects internal targets.
	var extra []uint16
	if port := req.URL.Port(); port != "" {
		if n, err := strconv.Atoi(port); err == nil && n != 80 && n != 443 {
			extra = []uint16{uint16(n)}
		}
	}
	res, err := s.outbound.Do(req, outbound.Policy{
		Purpose:     outbound.Webhook,
		Redirect:    outbound.RedirectNone,
		Timeout:     webhookSendTimeout,
		MaxBodySize: webhookResponseMax,
		ExtraPorts:  extra,
	})
	return classifyWebhook(res, err)
}

// resolveWebhookJob commits the delivery result. Only definite failures are
// rescheduled with exponential backoff; ambiguous outcomes are dropped, never
// retried, and do not count toward the disable threshold.
func (s *Service) resolveWebhookJob(ctx context.Context, jobID, webhookID int64, attempts int, r webhookResult, generation string) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		var current string
		if err := tx.QueryRow(ctx, "SELECT generation::text FROM system WHERE singleton").Scan(&current); err != nil {
			return err
		}
		if generation != current {
			return fmt.Errorf("webhook generation changed: %w", ErrGone)
		}

		switch r.outcome {
		case outcomeDelivered:
			if _, err := tx.Exec(ctx, "DELETE FROM jobs WHERE id=$1", jobID); err != nil {
				return fmt.Errorf("finish webhook job: %w", err)
			}
			if _, err := tx.Exec(ctx, "UPDATE webhooks SET consecutive_failures=0 WHERE id=$1 AND consecutive_failures<>0", webhookID); err != nil {
				return fmt.Errorf("reset webhook failures: %w", err)
			}
		case outcomeDefinite:
			runAt := s.now().UTC().Add(webhookBackoff(attempts, r.retryAfter))
			if _, err := tx.Exec(ctx, "UPDATE jobs SET run_at=$2,locked_until=NULL,last_error=$3 WHERE id=$1", jobID, runAt, r.class); err != nil {
				return fmt.Errorf("reschedule webhook job: %w", err)
			}
			var active bool
			err := tx.QueryRow(ctx, `UPDATE webhooks SET consecutive_failures=consecutive_failures+1,
    active = active AND consecutive_failures+1 < $2 WHERE id=$1 RETURNING active`, webhookID, webhookMaxFailures).Scan(&active)
			if errors.Is(err, pgx.ErrNoRows) {
				if _, err := tx.Exec(ctx, "DELETE FROM jobs WHERE id=$1", jobID); err != nil {
					return fmt.Errorf("drop webhook job: %w", err)
				}
				return nil
			}
			if err != nil {
				return fmt.Errorf("count webhook failure: %w", err)
			}
			if !active {
				if _, err := tx.Exec(ctx, "DELETE FROM jobs WHERE kind=$1 AND (payload->>'webhook_id')::bigint=$2", webhookJobKind, webhookID); err != nil {
					return fmt.Errorf("disable webhook jobs: %w", err)
				}
			}
		default:
			if _, err := tx.Exec(ctx, "DELETE FROM jobs WHERE id=$1", jobID); err != nil {
				return fmt.Errorf("drop webhook job: %w", err)
			}
		}
		return nil
	})
}
