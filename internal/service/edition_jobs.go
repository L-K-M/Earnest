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
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	editionJobLease            = 10 * time.Minute
	editionPollDelay           = time.Minute
	editionMaxSources          = 8
	editionMaxAttempts         = 3
	editionMaxClaim            = 1000
	editionWorkerLockID        = 570231120
	editionCancellationWarning = "Batch cancellation could not be confirmed; the provider may still charge for it."
)

type editionJob struct {
	UserID                  int64          `json:"user_id"`
	EditionID               int64          `json:"edition_id"`
	BatchID                 string         `json:"batch_id,omitempty"`
	BatchTried              bool           `json:"batch_tried,omitempty"`
	BatchSources            string         `json:"batch_sources,omitempty"`
	CancellationUnconfirmed bool           `json:"cancellation_unconfirmed,omitempty"`
	CompletedSections       map[int]string `json:"completed_sections,omitempty"`
}

type claimedEdition struct {
	id      int64
	attempt int
	lease   time.Time
	job     editionJob
}

// RunEditionJobs submits early batches, polls them and falls back to synchronous prose when due.
// Paused budgets retain jobs without consuming failure attempts.
func (s *Service) RunEditionJobs(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > editionMaxClaim {
		return 0, ErrInvalid
	}
	if !pipelineEngine(ctx).Enabled(llm.TaskEdition) {
		return 0, nil
	}
	// Match the pipeline worker's session lock: a slow provider can outlive a lease.
	conn, err := s.store.Pool().Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("edition worker connection: %w", err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, editionWorkerLockID).Scan(&locked); err != nil {
		return 0, fmt.Errorf("edition worker lock: %w", err)
	}
	if !locked {
		return 0, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, editionWorkerLockID); err != nil {
			conn.Conn().Close(unlockCtx)
		}
	}()
	processed := 0
	for processed < limit {
		job, err := s.claimEdition(ctx)
		if err != nil {
			return processed, err
		}
		if job == nil {
			break
		}
		processed++
		out, err := s.editionByID(ctx, job.job.UserID, job.job.EditionID)
		if err == nil {
			err = s.generateEdition(ctx, job, &out)
		}
		if err := s.finishEdition(ctx, job, &out, err); err != nil {
			return processed, err
		}
	}
	return processed, nil
}

func (s *Service) claimEdition(ctx context.Context) (*claimedEdition, error) {
	var out *claimedEdition
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var id int64
		var attempt int
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT id,attempts,payload FROM jobs WHERE kind='edition' AND run_at<=$1 AND (locked_until IS NULL OR locked_until<=$1) ORDER BY run_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, s.now()).Scan(&id, &attempt, &payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim edition: %w", err)
		}
		out = &claimedEdition{id: id, attempt: attempt + 1, lease: s.now().Add(editionJobLease)}
		if err := json.Unmarshal(payload, &out.job); err != nil {
			return fmt.Errorf("decode edition job: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE jobs SET attempts=$2,locked_until=$3 WHERE id=$1`, id, out.attempt, out.lease)
		if err != nil {
			return fmt.Errorf("lease edition: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Service) generateEdition(ctx context.Context, job *claimedEdition, out *Edition) error {
	engine := pipelineEngine(ctx)
	model, err := engine.Model(llm.TaskEdition)
	if err != nil {
		return pipelineError(err)
	}
	out.Model = model
	out.PromptVersion = pipeline.EditionPromptVersion
	out.Error = ""
	if job.job.CancellationUnconfirmed {
		out.Error = editionCancellationWarning
	}
	if job.job.CompletedSections == nil {
		job.job.CompletedSections = map[int]string{}
	}
	requests := []llm.CompleteRequest{}
	indexes := []int{}
	sets := [][]pipeline.Source{}
	fingerprints := []string{}
	for i := range out.Sections {
		section := &out.Sections[i]
		section.Sources = []pipeline.Source{}
		for j := range section.Items {
			item := &section.Items[j]
			hash, err := hex.DecodeString(item.ContentHash)
			if err != nil {
				return fmt.Errorf("edition source hash: %w", err)
			}
			source, err := s.store.EditionSource(ctx, job.job.UserID, item.EntryID, hash, item.ContentSource, item.SanitizerVersion)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if !source.Allowed {
				item.Summary = nil
				item.Blurb = nil
				continue
			}
			// Summaries are always made independently from each article's raw content.
			// They are never fed into the edition model as a second-order prompt.
			for _, kind := range []llm.CacheKind{llm.CacheBlurb, llm.CacheSummary} {
				task, version, annotationKind := llm.TaskSummary, pipeline.SummaryPromptVersion, AnnotationSummary
				if kind == llm.CacheBlurb {
					task, version, annotationKind = llm.TaskBlurb, pipeline.BlurbPromptVersion, AnnotationBlurb
				}
				if !engine.Enabled(task) {
					continue
				}
				claims, summaryModel, err := engine.Summarize(ctx, source.OwnerUserID, source.Text, kind)
				if err != nil {
					return pipelineError(err)
				}
				result := &SummaryResult{EntryID: item.EntryID, ContentHash: item.ContentHash, ContentSource: source.Source, SanitizerVersion: source.SanitizerVersion, Kind: annotationKind, Claims: claims, Model: summaryModel, PromptVersion: version, Untrusted: true, ExcerptNotice: item.ExcerptNotice}
				// Historical snapshots remain usable, but cannot replace a current annotation.
				if err := s.saveSummary(ctx, job.job.UserID, source, result); err != nil && !errors.Is(err, ErrConflict) {
					return err
				}
				if kind == llm.CacheBlurb {
					item.Blurb = result
				} else {
					item.Summary = result
				}
			}
			section.Sources = append(section.Sources, pipeline.Source{EntryID: item.EntryID, ContentHash: item.ContentHash, Text: askPassage(source.Text, ""), ExcerptOnly: source.ExcerptOnly})
			if len(section.Sources) == editionMaxSources {
				break
			}
		}
		fingerprintData, err := json.Marshal(struct {
			Model, PromptVersion string
			Sources              []pipeline.Source
		}{model, pipeline.EditionPromptVersion, section.Sources})
		if err != nil {
			return fmt.Errorf("edition section fingerprint: %w", err)
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(fingerprintData))
		if job.job.CompletedSections[i] == fingerprint && len(section.Claims) > 0 {
			continue
		}
		// Consent, content, model and prompt changes invalidate saved prose.
		delete(job.job.CompletedSections, i)
		section.Claims = []pipeline.GroundedClaim{}
		if len(section.Sources) == 0 {
			continue
		}
		fingerprints = append(fingerprints, fingerprint)
		period := "This week"
		if out.Period == EditionDaily {
			period = "Today"
		}
		req, err := pipeline.GroundedRequest(llm.TaskEdition, period, section.Sources)
		if err != nil {
			return err
		}
		requests = append(requests, req)
		indexes = append(indexes, i)
		sets = append(sets, section.Sources)
	}
	fingerprintData, err := json.Marshal(struct {
		Model, PromptVersion string
		Sources              [][]pipeline.Source
	}{model, pipeline.EditionPromptVersion, sets})
	if err != nil {
		return fmt.Errorf("edition batch fingerprint: %w", err)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(fingerprintData))
	batch := pipeline.EditionBatchFrom(ctx)
	if job.job.BatchID != "" && (job.job.BatchSources != fingerprint || batch == nil) {
		if batch == nil {
			job.job.CancellationUnconfirmed = true
			out.Error = editionCancellationWarning
		} else {
			if err := batch.Cancel(ctx, job.job.BatchID); err != nil {
				job.job.CancellationUnconfirmed = true
				out.Error = editionCancellationWarning
			}
		}
		job.job.BatchID = ""
		job.job.BatchSources = ""
	}
	if len(requests) == 0 {
		out.Status = "ready"
		return nil
	}
	if job.job.BatchID != "" && batch != nil {
		outputs, done, err := batch.Poll(ctx, job.job.BatchID, requests)
		if err == nil && done {
			if len(outputs) != len(requests) {
				return llm.ErrInvalidOutput
			}
			for j, text := range outputs {
				claims, err := pipeline.DecodeGrounded(requests[j], text, sets[j])
				if err != nil {
					return err
				}
				out.Sections[indexes[j]].Claims = claims
			}
			out.Status = "ready"
			return nil
		}
		// Keep the durable handle through transient polling failures until due.
		if s.now().Before(out.ReadyAt) {
			out.Status = "pending"
			return nil
		}
		// A missed cancellation must not prevent a due paper from using its fallback.
		if err := batch.Cancel(ctx, job.job.BatchID); err != nil {
			job.job.CancellationUnconfirmed = true
			out.Error = editionCancellationWarning
		}
		job.job.BatchID = ""
		job.job.BatchSources = ""
	}
	if s.now().Before(out.ReadyAt) {
		if batch != nil && !job.job.BatchTried {
			id, err := batch.Submit(ctx, requests)
			if errors.Is(err, llm.ErrBudgetExhausted) {
				return pipelineError(err)
			}
			job.job.BatchTried = true
			if err == nil {
				job.job.BatchID = id
				job.job.BatchSources = fingerprint
			} else if !errors.Is(err, llm.ErrUnsupported) {
				return err
			}
		}
		out.Status = "pending"
		return nil
	}
	for j, req := range requests {
		response, err := engine.Complete(ctx, llm.TaskEdition, req)
		if err != nil {
			return pipelineError(err)
		}
		claims, err := pipeline.DecodeGrounded(req, response.Text, sets[j])
		if err != nil {
			return err
		}
		out.Sections[indexes[j]].Claims = claims
		job.job.CompletedSections[indexes[j]] = fingerprints[j]
	}
	out.Status = "ready"
	return nil
}

func (s *Service) finishEdition(ctx context.Context, job *claimedEdition, out *Edition, cause error) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		var owns bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND attempts=$2 AND locked_until=$3 FOR UPDATE)`, job.id, job.attempt, job.lease).Scan(&owns); err != nil {
			return fmt.Errorf("edition lease check: %w", err)
		}
		if !owns {
			return nil
		}
		if errors.Is(cause, ErrNotFound) {
			_, err := tx.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, job.id)
			return err
		}
		if cause != nil {
			out.Status = "pending"
			out.Error = "Edition generation will retry."
			if pipelineWaiting(cause) {
				out.Status = "paused"
				out.Error = "Edition generation is paused by its budget."
			}
			if !pipelineWaiting(cause) && job.attempt >= editionMaxAttempts {
				out.Status = "failed"
				out.Error = "Edition generation failed. The original list is available."
			}
		}
		if job.job.CancellationUnconfirmed {
			if out.Error == "" {
				out.Error = editionCancellationWarning
			} else if out.Error != editionCancellationWarning {
				out.Error += " " + editionCancellationWarning
			}
		}
		if cause != nil && len(out.Sections) == 0 {
			// A failed load has no snapshot to save; retain the stored list and provenance.
			if _, err := tx.Exec(ctx, `UPDATE editions SET status=$3,error=$4 WHERE user_id=$1 AND id=$2`, job.job.UserID, job.job.EditionID, out.Status, out.Error); err != nil {
				return fmt.Errorf("save edition failure: %w", err)
			}
		} else {
			data, err := json.Marshal(out.Sections)
			if err != nil {
				return fmt.Errorf("encode generated edition: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE editions SET status=$3,sections=$4,model=$5,error=$6,prompt_version=$7 WHERE user_id=$1 AND id=$2`, job.job.UserID, job.job.EditionID, out.Status, data, out.Model, out.Error, out.PromptVersion); err != nil {
				return fmt.Errorf("save edition: %w", err)
			}
		}
		if out.Status == "ready" || out.Status == "failed" {
			if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, job.id); err != nil {
				return fmt.Errorf("finish edition job: %w", err)
			}
			if out.Status == "ready" {
				return s.emitWebhooks(ctx, tx, job.job.UserID, WebhookEvent{Type: WebhookEditionReady, EditionID: out.ID})
			}
			return nil
		}
		payload, err := json.Marshal(job.job)
		if err != nil {
			return fmt.Errorf("edition job state: %w", err)
		}
		attempts := job.attempt
		if cause == nil || pipelineWaiting(cause) {
			attempts--
		}
		next := s.now().Add(editionPollDelay)
		if cause == nil && job.job.BatchID == "" && out.ReadyAt.After(s.now()) {
			next = out.ReadyAt
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET payload=$2,attempts=$3,locked_until=NULL,run_at=$4,last_error=$5 WHERE id=$1`, job.id, payload, attempts, next, out.Error); err != nil {
			return fmt.Errorf("reschedule edition: %w", err)
		}
		return nil
	})
}
