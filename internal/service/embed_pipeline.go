package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

type pipelineKey struct{}

// WithPipeline attaches immutable runtime dependencies to a server/worker
// context. A context without an engine leaves every model task disabled.
func WithPipeline(ctx context.Context, engine *pipeline.Engine) context.Context {
	return context.WithValue(ctx, pipelineKey{}, engine)
}
func pipelineEngine(ctx context.Context) *pipeline.Engine {
	engine, _ := ctx.Value(pipelineKey{}).(*pipeline.Engine)
	return engine
}

var (
	// ErrPipelineDisabled reports an unconfigured task to interactive clients.
	ErrPipelineDisabled = errors.New("pipeline disabled")
	// ErrPipelinePaused leaves durable work pending until its budget is available.
	ErrPipelinePaused = errors.New("pipeline paused")
)

func pipelineError(err error) error {
	if errors.Is(err, llm.ErrDisabled) {
		return fmt.Errorf("%w: %w", ErrPipelineDisabled, err)
	}
	if errors.Is(err, llm.ErrBudgetExhausted) {
		return fmt.Errorf("%w: %w", ErrPipelinePaused, err)
	}
	return err
}

// PipelineStatus describes configuration without making any provider calls.
type PipelineStatus struct {
	Embed       string `json:"embed"`
	Classify    string `json:"classify"`
	Semantic    string `json:"semantic"`
	Watch       string `json:"watch"`
	Blurb       string `json:"blurb"`
	Summary     string `json:"summary"`
	Edition     string `json:"edition"`
	Ask         string `json:"ask"`
	CompileRule string `json:"compile_rule"`
}

// PipelineStatus reports disabled tasks explicitly for the UI.
func (s *Service) PipelineStatus(ctx context.Context, a Actor) (PipelineStatus, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return PipelineStatus{}, err
	}
	status := PipelineStatus{Embed: "disabled", Classify: "disabled", Semantic: "disabled", Watch: "disabled", Blurb: "disabled", Summary: "disabled", Edition: "disabled", Ask: "disabled", CompileRule: "disabled"}
	e := pipelineEngine(ctx)
	if e.Enabled(llm.TaskEmbed) {
		status.Embed = "enabled"
		status.Semantic = "enabled"
		status.Watch = "enabled"
	}
	if e.Enabled(llm.TaskClassify) {
		status.Classify = "enabled"
	}
	for task, target := range map[llm.Task]*string{llm.TaskBlurb: &status.Blurb, llm.TaskSummary: &status.Summary, llm.TaskEdition: &status.Edition, llm.TaskAsk: &status.Ask, llm.TaskCompileRule: &status.CompileRule} {
		if e.Enabled(task) {
			*target = "enabled"
		}
	}
	return status, nil
}

// RunPipeline backfills bounded batches and then clusters publisher entries.
// Pending rows are the durable work queue; no completion is recorded on pause.
func (s *Service) RunPipeline(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > maxClusterPending {
		return 0, ErrInvalid
	}
	engine := pipelineEngine(ctx)
	if !engine.Enabled(llm.TaskEmbed) && !engine.Enabled(llm.TaskClassify) {
		return 0, nil
	}
	// One worker per database prevents duplicate calls from overlapping servers.
	conn, err := s.store.Pool().Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("pipeline worker connection: %w", err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(570231119)`).Scan(&locked); err != nil {
		return 0, fmt.Errorf("pipeline worker lock: %w", err)
	}
	if !locked {
		return 0, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(570231119)`); err != nil {
			conn.Conn().Close(unlockCtx)
		}
	}()
	processed := 0
	if engine.Enabled(llm.TaskEmbed) {
		model, _ := engine.Model(llm.TaskEmbed)
		entries, err := s.store.EmbedPending(ctx, model, limit)
		if err != nil {
			return processed, err
		}
		for _, entry := range entries {
			consent, err := s.store.EmbedConsent(ctx, entry.ID)
			if err != nil {
				return processed, err
			}
			if !consent {
				continue
			}
			content := publisherContent(entry)
			hash := content.Hash()
			dims, values, found, err := s.store.EmbedCached(ctx, entry.OwnerUserID, hash[:], model)
			if err != nil {
				return processed, err
			}
			if !found {
				vector, err := engine.Embed(ctx, content)
				if errors.Is(err, llm.ErrBudgetExhausted) {
					break
				}
				if err != nil {
					return processed, err
				}
				dims, values = vector.SourceDimensions, vector.Values
			}
			if err := s.store.EmbedSave(ctx, entry, model, hash[:], dims, values); err != nil {
				return processed, err
			}
			processed++
		}
		if _, err := s.ClusterPending(ctx, model, limit); err != nil {
			return processed, err
		}
	}
	if engine.Enabled(llm.TaskClassify) {
		model, _ := engine.Model(llm.TaskClassify)
		entries, err := s.store.ClassifyPending(ctx, model, pipeline.ClassifierPromptVersion, limit)
		if err != nil {
			return processed, err
		}
		for _, entry := range entries {
			consent, err := s.store.EmbedConsent(ctx, entry.ID)
			if err != nil {
				return processed, err
			}
			if !consent {
				continue
			}
			content := publisherContent(entry)
			labels, err := engine.Classify(ctx, content)
			if errors.Is(err, llm.ErrBudgetExhausted) {
				break
			}
			if err != nil {
				if deferErr := s.deferClassifier(ctx, entry, model); deferErr != nil {
					return processed, deferErr
				}
				continue
			}
			hash := content.Hash()
			if err := s.store.ClassifySave(ctx, entry, model, pipeline.ClassifierPromptVersion, hash[:], labels); err != nil {
				return processed, err
			}
			if _, err := s.store.Pool().Exec(ctx, `DELETE FROM jobs WHERE unique_key=$1 AND kind='classify'`, classifierRetryKey(entry.ID)); err != nil {
				return processed, fmt.Errorf("clear classifier retry: %w", err)
			}
			processed++
		}
	}
	return processed, nil
}

func publisherContent(e store.EmbedEntry) pipeline.Content {
	return pipeline.Content{OwnerUserID: e.OwnerUserID, Title: e.Title, Text: e.Text}
}

// FeedPipelineSettings exposes only consent, never feed credentials.
type FeedPipelineSettings struct {
	FeedID  int64 `json:"feed_id"`
	Allowed bool  `json:"allowed"`
}

// FeedLLMConsent reads the owner's per-feed provider permission.
func (s *Service) FeedLLMConsent(ctx context.Context, a Actor, feed int64) (FeedPipelineSettings, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return FeedPipelineSettings{}, err
	}
	out := FeedPipelineSettings{FeedID: feed}
	err := s.store.Pool().QueryRow(ctx, `SELECT f.llm_allowed FROM feeds f JOIN subscriptions s ON s.feed_id=f.id AND s.user_id=$1 AND s.deleted_at IS NULL WHERE f.id=$2 AND f.owner_user_id=$1`, a.UserID, feed).Scan(&out.Allowed)
	return out, domainError(err)
}

// SetFeedLLMConsent requires an explicit human action. Opting out immediately
// excludes existing vectors and labels from all provider-derived predicates.
func (s *Service) SetFeedLLMConsent(ctx context.Context, a Actor, feed int64, allowed bool, opID string) (FeedPipelineSettings, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return FeedPipelineSettings{}, err
	}
	if feed <= 0 || !validOpID(opID) {
		return FeedPipelineSettings{}, ErrInvalid
	}
	out := FeedPipelineSettings{FeedID: feed, Allowed: allowed}
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "SetFeedLLMConsent", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		tag, err := tx.Exec(ctx, `UPDATE feeds f SET llm_allowed=$3 WHERE f.id=$2 AND f.owner_user_id=$1 AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.user_id=$1 AND s.deleted_at IS NULL)`, a.UserID, feed, allowed)
		if err != nil {
			return fmt.Errorf("set provider consent: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	return out, err
}
