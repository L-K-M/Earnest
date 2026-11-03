package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	ruleJobKind        = "rule"
	ruleJobLease       = 5 * time.Minute
	ruleJobMaxAttempts = 3
	ruleJobMaxClaim    = 1000
	ruleJobRetryDelay  = time.Minute
)

type ruleJob struct {
	UserID   int64   `json:"user_id"`
	RuleID   int64   `json:"rule_id"`
	FeedID   int64   `json:"feed_id"`
	EntryIDs []int64 `json:"entry_ids"`
}

type claimedRuleJob struct {
	id      int64
	attempt int
	lease   time.Time
	payload ruleJob
}

// enqueueRuleJobs snapshots the enabled rules for this ingest in the same transaction.
func enqueueRuleJobs(ctx context.Context, tx pgx.Tx, feed int64, ids []int64, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO jobs(kind,payload,run_at)
 SELECT $1,jsonb_build_object('user_id',r.user_id,'rule_id',r.id,'feed_id',$2::bigint,'entry_ids',to_jsonb($3::bigint[])),$4
 FROM rules r WHERE r.enabled AND EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id=r.user_id AND s.feed_id=$2 AND s.deleted_at IS NULL)`, ruleJobKind, feed, ids, now)
	if err != nil {
		return fmt.Errorf("enqueue rule jobs: %w", err)
	}
	return nil
}

// RunRuleJobs processes due rule jobs, rescheduling failed attempts with backoff.
func (s *Service) RunRuleJobs(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > ruleJobMaxClaim {
		return 0, ErrInvalid
	}
	processed := 0
	for processed < limit {
		job, err := s.claimRuleJob(ctx)
		if err != nil {
			return processed, err
		}
		if job == nil {
			break
		}
		processed++
		err = s.executeRuleJob(ctx, job.payload.UserID, job.payload.RuleID, job.payload.FeedID, job.payload.EntryIDs)
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		if finishErr := s.finishRuleJob(ctx, job, err); finishErr != nil {
			return processed, finishErr
		}
	}
	return processed, nil
}

func (s *Service) claimRuleJob(ctx context.Context) (*claimedRuleJob, error) {
	var job *claimedRuleJob
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var id int64
		var attempts int
		var raw []byte
		now := s.now().UTC()
		err := tx.QueryRow(ctx, `SELECT id,attempts,payload FROM jobs WHERE kind=$1 AND run_at<=$2 AND (locked_until IS NULL OR locked_until<=$2) ORDER BY run_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, ruleJobKind, now).Scan(&id, &attempts, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim rule job: %w", err)
		}
		job = &claimedRuleJob{id: id, attempt: attempts + 1, lease: now.Add(ruleJobLease)}
		if err := json.Unmarshal(raw, &job.payload); err != nil {
			return fmt.Errorf("decode rule job: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET attempts=$2,locked_until=$3 WHERE id=$1`, id, job.attempt, job.lease); err != nil {
			return fmt.Errorf("lease rule job: %w", err)
		}
		return nil
	})
	return job, err
}

func (s *Service) finishRuleJob(ctx context.Context, job *claimedRuleJob, cause error) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND attempts=$2 AND locked_until=$3 FOR UPDATE)`, job.id, job.attempt, job.lease).Scan(&exists); err != nil {
			return fmt.Errorf("check rule job lease: %w", err)
		}
		if !exists {
			return nil
		}
		if pipelineWaiting(cause) {
			if _, err := tx.Exec(ctx, `UPDATE jobs SET attempts=greatest(attempts-1,0),run_at=$2,locked_until=NULL,last_error='pipeline paused' WHERE id=$1`, job.id, s.now().UTC().Add(ruleJobRetryDelay)); err != nil {
				return fmt.Errorf("pause rule job: %w", err)
			}
			return nil
		}
		if cause == nil {
			_, err := tx.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, job.id)
			if err != nil {
				return fmt.Errorf("complete rule job: %w", err)
			}
			return nil
		}
		reason := cause.Error()
		if len(reason) > 1024 {
			reason = reason[:1024]
		}
		if job.attempt >= ruleJobMaxAttempts {
			if _, err := tx.Exec(ctx, `UPDATE rules SET enabled=false,action=jsonb_set(action,'{disable_reason}',to_jsonb($3::text)) WHERE id=$1 AND user_id=$2 AND enabled`, job.payload.RuleID, job.payload.UserID, reason); err != nil {
				return fmt.Errorf("disable failing rule: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, job.id); err != nil {
				return fmt.Errorf("drop failing rule job: %w", err)
			}
			return nil
		}
		runAt := s.now().UTC().Add(ruleJobRetryDelay * time.Duration(1<<(job.attempt-1)))
		if _, err := tx.Exec(ctx, `UPDATE jobs SET run_at=$2,locked_until=NULL,last_error=$3 WHERE id=$1`, job.id, runAt, reason); err != nil {
			return fmt.Errorf("retry rule job: %w", err)
		}
		return nil
	})
}
