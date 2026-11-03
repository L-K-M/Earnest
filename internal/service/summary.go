package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// SummaryResult is tainted plain text, with verified passages of one stored version.
type SummaryResult struct {
	AnnotationID     int64            `json:"annotation_id"`
	EntryID          int64            `json:"entry_id"`
	ContentHash      string           `json:"content_hash"`
	ContentSource    string           `json:"content_source"`
	SanitizerVersion int              `json:"sanitizer_version"`
	Kind             AnnotationKind   `json:"kind"`
	Model            string           `json:"model"`
	PromptVersion    string           `json:"prompt_version"`
	Claims           []pipeline.Claim `json:"claims"`
	ExcerptNotice    string           `json:"excerpt_notice,omitempty"`
	Untrusted        bool             `json:"untrusted"`
}

// SummarizeEntry generates lazily; no annotation or external-agent note enters the prompt.
func (s *Service) SummarizeEntry(ctx context.Context, a Actor, id int64, kind AnnotationKind) (SummaryResult, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return SummaryResult{}, err
	}
	if id <= 0 {
		return SummaryResult{}, ErrInvalid
	}
	if kind == "" {
		kind = AnnotationSummary
	}
	task, cacheKind, version := llm.TaskSummary, llm.CacheSummary, pipeline.SummaryPromptVersion
	if kind == AnnotationBlurb {
		task, cacheKind, version = llm.TaskBlurb, llm.CacheBlurb, pipeline.BlurbPromptVersion
	} else if kind != AnnotationSummary {
		return SummaryResult{}, ErrInvalid
	}
	engine := pipelineEngine(ctx)
	if !engine.Enabled(task) {
		return SummaryResult{}, ErrPipelineDisabled
	}
	source, err := s.store.SummaryContent(ctx, a.UserID, id)
	if err != nil {
		return SummaryResult{}, domainError(err)
	}
	if !source.Allowed {
		return SummaryResult{}, fmt.Errorf("private feed requires provider consent: %w", ErrForbidden)
	}
	claims, model, err := engine.Summarize(ctx, source.OwnerUserID, source.Text, cacheKind)
	if err != nil {
		return SummaryResult{}, pipelineError(err)
	}
	out := SummaryResult{EntryID: id, ContentHash: hex.EncodeToString(source.ContentHash), ContentSource: source.Source, SanitizerVersion: source.SanitizerVersion, Kind: kind, Model: model, PromptVersion: version, Claims: claims, Untrusted: true}
	if source.ExcerptOnly {
		out.ExcerptNotice = pipeline.ExcerptNotice
	}
	if err := s.saveSummary(ctx, a.UserID, source, &out); err != nil {
		return SummaryResult{}, err
	}
	return out, nil
}

func (s *Service) saveSummary(ctx context.Context, user int64, source store.SummarySource, out *SummaryResult) error {
	text := make([]string, 0, len(out.Claims))
	for _, claim := range out.Claims {
		text = append(text, claim.Text)
	}
	value := strings.Join(text, " ")
	value, err := plainText(value, annotationLimit(out.Kind))
	if err != nil {
		return fmt.Errorf("generated annotation: %w", err)
	}
	citations, err := json.Marshal(out.Claims)
	if err != nil {
		return fmt.Errorf("encode summary citations: %w", err)
	}
	return s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		// Consent and version may change while the provider is working.
		var current bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entries e JOIN feeds f ON f.id=e.feed_id JOIN subscriptions sub ON sub.feed_id=f.id AND sub.user_id=$1 AND sub.deleted_at IS NULL WHERE e.id=$2 AND e.deleted_at IS NULL AND e.content_hash=$3 AND (f.owner_user_id IS NULL OR (f.owner_user_id=$1 AND f.llm_allowed)))`, user, source.EntryID, source.ContentHash).Scan(&current); err != nil {
			return fmt.Errorf("check summary version: %w", err)
		}
		if !current {
			return ErrConflict
		}
		var principal int64
		if err := tx.QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Summaries') ON CONFLICT(user_id,name) WHERE kind='pipeline' AND name='Summaries' DO UPDATE SET name=EXCLUDED.name RETURNING id`, user).Scan(&principal); err != nil {
			return fmt.Errorf("summary principal: %w", err)
		}
		var oldID int64
		err := tx.QueryRow(ctx, `SELECT id FROM annotations WHERE user_id=$1 AND principal_id=$2 AND kind=$3 AND entry_ids=ARRAY[$4::bigint] AND model=$5 AND prompt_version=$6 AND content_hash=$7 AND citations=$8::jsonb AND value=$9 AND deleted_at IS NULL`, user, principal, out.Kind, source.EntryID, out.Model, out.PromptVersion, source.ContentHash, citations, value).Scan(&oldID)
		if err == nil {
			out.AnnotationID = oldID
			return nil
		}
		if err != pgx.ErrNoRows {
			return fmt.Errorf("existing summary: %w", err)
		}
		rev, err := changeRev(ctx, tx, user)
		if err != nil {
			return err
		}
		var liveID int64
		err = tx.QueryRow(ctx, `SELECT id FROM annotations WHERE user_id=$1 AND principal_id=$2 AND kind=$3 AND entry_ids=ARRAY[$4::bigint] AND deleted_at IS NULL ORDER BY id DESC LIMIT 1`, user, principal, out.Kind, source.EntryID).Scan(&liveID)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("find summary annotation: %w", err)
		}
		if liveID > 0 {
			out.AnnotationID = liveID
			if _, err := tx.Exec(ctx, `UPDATE annotations SET value=$2,model=$3,prompt_version=$4,rev=$5,content_hash=$6,content_source=$7,sanitizer_version=$8,citations=$9,created_at=$10 WHERE id=$1`, liveID, value, out.Model, out.PromptVersion, rev, source.ContentHash, source.Source, source.SanitizerVersion, citations, s.now()); err != nil {
				return fmt.Errorf("update summary annotation: %w", err)
			}
		} else if err := tx.QueryRow(ctx, `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,model,prompt_version,rev,content_hash,content_source,sanitizer_version,citations,created_at) VALUES($1,$2,$3,$4,ARRAY[$5::bigint],$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, user, principal, out.Kind, value, source.EntryID, out.Model, out.PromptVersion, rev, source.ContentHash, source.Source, source.SanitizerVersion, citations, s.now()).Scan(&out.AnnotationID); err != nil {
			return fmt.Errorf("save summary annotation: %w", err)
		}
		annotation := Annotation{ID: out.AnnotationID, PrincipalID: principal, Kind: out.Kind, EntryIDs: []int64{source.EntryID}, Value: value, Rev: rev, Untrusted: true}
		payload := annotationPayload(annotation)
		payload["generated"] = out
		return insertEvent(ctx, tx, user, rev, string(syncmodel.AnnotationUpdated), payload, s.now())
	})
}
