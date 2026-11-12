package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/jackc/pgx/v5"
)

const frontSummaryVersion = "front-page-v1"
const frontSummaryQuestion = "Summarize this story in two or three short sentences. Describe only this event, distinguish disagreements, and cite the supplied coverage."
const frontSummaryWorkerLock int64 = 570231121

type frontSummaryJob struct {
	UserID int64   `json:"user_id"`
	IDs    []int64 `json:"ids"`
	Key    string  `json:"key"`
	Model  string  `json:"model"`
}

func isMissingFrontSource(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func frontSummaryKey(model string, sources []pipeline.Source) string {
	raw, _ := json.Marshal(struct {
		Model, Version string
		Sources        []pipeline.Source
	}{model, frontSummaryVersion, sources})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// Rechecking subscription, mute and consent rules also prevents cached prose
// from retaining a source the reader has removed since its generation.
func (s *Service) frontSummarySources(ctx context.Context, user int64, ids []int64) ([]pipeline.Source, error) {
	out := []pipeline.Source{}
	for _, id := range ids {
		var visible bool
		err := s.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entries e JOIN subscriptions sub ON sub.feed_id=e.feed_id AND sub.user_id=$1 AND sub.deleted_at IS NULL WHERE e.id=$2 AND e.deleted_at IS NULL AND NOT sub.muted AND NOT EXISTS (SELECT 1 FROM mutes m WHERE m.user_id=$1 AND m.deleted_at IS NULL AND ((m.kind='author' AND lower(m.value)=lower(e.author)) OR (m.kind='keyword' AND position(lower(m.value) in lower(e.title))>0))))`, user, id).Scan(&visible)
		if err != nil {
			return nil, err
		}
		if !visible {
			continue
		}
		source, err := s.store.SummaryContent(ctx, user, id)
		if isMissingFrontSource(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !source.Allowed {
			continue
		}
		out = append(out, pipeline.Source{EntryID: id, ContentHash: hex.EncodeToString(source.ContentHash), Text: askPassage(source.Text, ""), ExcerptOnly: source.ExcerptOnly})
	}
	return out, nil
}

func (s *Service) frontPageSummary(ctx context.Context, a Actor, coverage []FrontPageCoverage) (*FrontPageSummary, error) {
	engine := pipelineEngine(ctx)
	if !engine.Enabled(llm.TaskEdition) {
		return nil, nil
	}
	model, err := engine.Model(llm.TaskEdition)
	if err != nil {
		return nil, nil
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, m := range coverage {
		if !seen[m.Entry.FeedID] {
			ids = append(ids, m.Entry.ID)
			seen[m.Entry.FeedID] = true
		}
		if len(ids) == 5 {
			break
		}
	}
	sources, err := s.frontSummarySources(ctx, a.UserID, ids)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}
	key := frontSummaryKey(model, sources)
	var raw []byte
	var retry time.Time
	err = s.store.Pool().QueryRow(ctx, `SELECT result,retry_at FROM front_page_summaries WHERE user_id=$1 AND cache_key=$2`, a.UserID, key).Scan(&raw, &retry)
	if err == nil {
		if len(raw) > 0 {
			var out FrontPageSummary
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, err
			}
			return &out, nil
		}
		if retry.After(s.now()) {
			return nil, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// Agent reads cannot enqueue provider spending on the human's behalf.
	if a.Kind != PrincipalHuman {
		return nil, nil
	}
	ids = nil
	for _, source := range sources {
		ids = append(ids, source.EntryID)
	}
	job := frontSummaryJob{UserID: a.UserID, IDs: ids, Key: key, Model: model}
	payload, err := json.Marshal(job)
	if err != nil {
		return nil, err
	}
	_, err = s.store.Pool().Exec(ctx, `INSERT INTO jobs(kind,payload,unique_key,run_at) VALUES('front_summary',$1,$2,$3) ON CONFLICT(unique_key) DO NOTHING`, payload, fmt.Sprintf("front_summary/%d/%s", a.UserID, key), s.now())
	return nil, err
}

// RunFrontPageJobs uses the same governed Edition provider as legacy editions.
// The request only enqueues work; a provider outage never prevents the paper
// from showing its original headlines and publisher excerpts.
func (s *Service) RunFrontPageJobs(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > editionMaxClaim {
		return 0, ErrInvalid
	}
	engine := pipelineEngine(ctx)
	if !engine.Enabled(llm.TaskEdition) {
		return 0, nil
	}
	conn, err := s.store.Pool().Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, frontSummaryWorkerLock).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(c, `SELECT pg_advisory_unlock($1)`, frontSummaryWorkerLock); err != nil {
			conn.Conn().Close(c)
		}
	}()
	if _, err := conn.Exec(ctx, `DELETE FROM front_page_summaries WHERE created_at<$1`, s.now().Add(-30*24*time.Hour)); err != nil {
		return 0, err
	}
	processed := 0
	for processed < limit {
		var id int64
		var attempts int
		var payload []byte
		err := conn.QueryRow(ctx, `UPDATE jobs SET attempts=attempts+1,locked_until=$2 WHERE id=(SELECT id FROM jobs WHERE kind='front_summary' AND run_at<=$1 AND (locked_until IS NULL OR locked_until<=$1) ORDER BY run_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id,attempts,payload`, s.now(), s.now().Add(editionJobLease)).Scan(&id, &attempts, &payload)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return processed, err
		}
		var job frontSummaryJob
		if err := json.Unmarshal(payload, &job); err != nil {
			return processed, err
		}
		result, cause := s.generateFrontSummary(ctx, job)
		processed++
		if cause != nil {
			if pipelineWaiting(cause) || attempts < editionMaxAttempts {
				if pipelineWaiting(cause) {
					attempts--
				}
				_, err = conn.Exec(ctx, `UPDATE jobs SET attempts=$2,locked_until=NULL,run_at=$3,last_error='Front page summary will retry.' WHERE id=$1`, id, attempts, s.now().Add(time.Minute))
				if err != nil {
					return processed, err
				}
				continue
			}
		}
		// A stale job is removed without publishing. A failed generation has a
		// cooldown, so repeated page loads cannot create an unbounded retry loop.
		tx, err := conn.Begin(ctx)
		if err != nil {
			return processed, err
		}
		if result != nil || cause != nil {
			var raw []byte
			if result != nil {
				raw, err = json.Marshal(result)
			}
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO front_page_summaries(user_id,cache_key,result,retry_at,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,cache_key) DO UPDATE SET result=EXCLUDED.result,retry_at=EXCLUDED.retry_at,created_at=EXCLUDED.created_at`, job.UserID, job.Key, raw, s.now().Add(time.Hour), s.now())
			}
		}
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, id)
		}
		if err != nil {
			tx.Rollback(ctx)
			return processed, err
		}
		if err := tx.Commit(ctx); err != nil {
			return processed, err
		}
	}
	return processed, nil
}

func (s *Service) generateFrontSummary(ctx context.Context, job frontSummaryJob) (*FrontPageSummary, error) {
	engine := pipelineEngine(ctx)
	model, err := engine.Model(llm.TaskEdition)
	if err != nil {
		return nil, err
	}
	if model != job.Model {
		return nil, nil
	}
	sources, err := s.frontSummarySources(ctx, job.UserID, job.IDs)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 || frontSummaryKey(model, sources) != job.Key {
		return nil, nil
	}
	// A GET that observed a miss just before another worker committed may
	// enqueue after that commit. Recheck here before spending again.
	var cached bool
	if err := s.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM front_page_summaries WHERE user_id=$1 AND cache_key=$2 AND (result IS NOT NULL OR retry_at>$3))`, job.UserID, job.Key, s.now()).Scan(&cached); err != nil {
		return nil, err
	}
	if cached {
		return nil, nil
	}
	req, err := pipeline.GroundedRequest(llm.TaskEdition, frontSummaryQuestion, sources)
	if err != nil {
		return nil, err
	}
	result, err := engine.Complete(ctx, llm.TaskEdition, req)
	if err != nil {
		return nil, pipelineError(err)
	}
	claims, err := pipeline.DecodeGrounded(req, result.Text, sources)
	if err != nil {
		return nil, err
	}
	// Do not promote a citation mismatch into the front-page summary.
	if len(claims) > 3 {
		return nil, llm.ErrInvalidOutput
	}
	for _, claim := range claims {
		if !claim.Verified {
			return nil, llm.ErrInvalidOutput
		}
	}
	current, err := s.frontSummarySources(ctx, job.UserID, job.IDs)
	if err != nil {
		return nil, err
	}
	if frontSummaryKey(model, current) != job.Key {
		return nil, nil
	}
	out := &FrontPageSummary{Claims: claims, Model: model, PromptVersion: frontSummaryVersion, GeneratedAt: s.now().UTC()}
	for _, source := range sources {
		if source.ExcerptOnly {
			out.ExcerptNotice = "Some sources gave excerpts only."
			break
		}
	}
	return out, nil
}
