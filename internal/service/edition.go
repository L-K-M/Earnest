package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// EditionPeriod selects the ingest-time window used by a paper.
type EditionPeriod string

const (
	EditionDaily  EditionPeriod = "daily"
	EditionWeekly EditionPeriod = "weekly"
)

// EditionItem preserves publisher titles and the exact content version selected.
type EditionItem struct {
	EntryID          int64          `json:"entry_id"`
	Title            string         `json:"title"`
	Summary          *SummaryResult `json:"summary,omitempty"`
	Blurb            *SummaryResult `json:"blurb,omitempty"`
	ClusterID        int64          `json:"cluster_id,omitempty"`
	AlsoIn           []int64        `json:"also_in"`
	ContentHash      string         `json:"content_hash"`
	ContentSource    string         `json:"content_source"`
	SanitizerVersion int            `json:"sanitizer_version"`
	ExcerptNotice    string         `json:"excerpt_notice,omitempty"`
}

// EditionSection contains a deterministic list and optional tainted prose.
type EditionSection struct {
	Name    string                   `json:"name"`
	Items   []EditionItem            `json:"items"`
	Claims  []pipeline.GroundedClaim `json:"claims"`
	Sources []pipeline.Source        `json:"sources"`
}

// Edition always retains its deterministic list, including while paused or disabled.
type Edition struct {
	ID            int64            `json:"id"`
	Period        EditionPeriod    `json:"period"`
	Status        string           `json:"status"`
	Sections      []EditionSection `json:"sections"`
	Model         string           `json:"model"`
	PromptVersion string           `json:"prompt_version"`
	ReadyAt       time.Time        `json:"ready_at"`
	CreatedAt     time.Time        `json:"created_at"`
	Error         string           `json:"error,omitempty"`
	Untrusted     bool             `json:"untrusted"`
}

// TriggerEdition snapshots a paper and queues generation immediately, ahead of readyAt.
// A zero readyAt means synchronous fallback is due on the next worker pass.
func (s *Service) TriggerEdition(ctx context.Context, a Actor, period EditionPeriod, readyAt time.Time, opID string) (Edition, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return Edition{}, err
	}
	if !validOpID(opID) || (period != EditionDaily && period != EditionWeekly) {
		return Edition{}, ErrInvalid
	}
	now := s.now().UTC()
	if readyAt.IsZero() {
		readyAt = now
	}
	window := 24 * time.Hour
	if period == EditionWeekly {
		window *= 7
	}
	candidates, err := s.store.EditionCandidates(ctx, a.UserID, now.Add(-window), now)
	if err != nil {
		return Edition{}, err
	}
	sections := editionSections(candidates)
	out := Edition{Period: period, Status: "list", Sections: sections, PromptVersion: pipeline.EditionPromptVersion, ReadyAt: readyAt, CreatedAt: now, Untrusted: true}
	if pipelineEngine(ctx).Enabled(llm.TaskEdition) && len(sections) > 0 {
		out.Status = "pending"
		out.Model, _ = pipelineEngine(ctx).Model(llm.TaskEdition)
	}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "TriggerEdition", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		if readyAt.Before(now) || readyAt.After(now.Add(7*24*time.Hour)) {
			return ErrInvalid
		}
		data, err := json.Marshal(sections)
		if err != nil {
			return fmt.Errorf("encode edition: %w", err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO editions(user_id,period,status,sections,model,prompt_version,ready_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, a.UserID, period, out.Status, data, out.Model, out.PromptVersion, readyAt, now).Scan(&out.ID); err != nil {
			return fmt.Errorf("create edition: %w", err)
		}
		if out.Status == "pending" {
			payload, err := json.Marshal(editionJob{UserID: a.UserID, EditionID: out.ID})
			if err != nil {
				return fmt.Errorf("encode edition job: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO jobs(kind,payload,unique_key,run_at) VALUES('edition',$1,$2,$3)`, payload, fmt.Sprintf("edition/%d", out.ID), now); err != nil {
				return fmt.Errorf("enqueue edition: %w", err)
			}
		} else if err := s.emitWebhooks(ctx, tx, a.UserID, WebhookEvent{Type: WebhookEditionReady, EditionID: out.ID}); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	return out, err
}

func editionSections(candidates []store.EditionCandidate) []EditionSection {
	sections := []EditionSection{}
	positions := map[string]int{}
	for _, e := range candidates {
		pos, ok := positions[e.Folder]
		if !ok {
			pos = len(sections)
			positions[e.Folder] = pos
			sections = append(sections, EditionSection{Name: e.Folder, Items: []EditionItem{}, Claims: []pipeline.GroundedClaim{}, Sources: []pipeline.Source{}})
		}
		section := &sections[pos]
		merged := false
		if e.ClusterID > 0 {
			for i := range section.Items {
				if section.Items[i].ClusterID == e.ClusterID {
					section.Items[i].AlsoIn = append(section.Items[i].AlsoIn, e.ID)
					merged = true
					break
				}
			}
		}
		if merged {
			continue
		}
		item := EditionItem{EntryID: e.ID, Title: e.Title, ClusterID: e.ClusterID, AlsoIn: []int64{}, ContentHash: hex.EncodeToString(e.ContentHash), ContentSource: e.ContentSource, SanitizerVersion: e.SanitizerVersion}
		if e.ExcerptOnly {
			item.ExcerptNotice = pipeline.ExcerptNotice
		}
		section.Items = append(section.Items, item)
	}
	return sections
}

func (s *Service) editionByID(ctx context.Context, user, id int64) (Edition, error) {
	var out Edition
	var data []byte
	err := s.store.Pool().QueryRow(ctx, `SELECT id,period,status,sections,model,prompt_version,ready_at,created_at,error FROM editions WHERE user_id=$1 AND ($2::bigint=0 OR id=$2) ORDER BY created_at DESC,id DESC LIMIT 1`, user, id).Scan(&out.ID, &out.Period, &out.Status, &data, &out.Model, &out.PromptVersion, &out.ReadyAt, &out.CreatedAt, &out.Error)
	if err != nil {
		return out, domainError(err)
	}
	if err := json.Unmarshal(data, &out.Sections); err != nil {
		return out, fmt.Errorf("decode edition: %w", err)
	}
	out.Untrusted = true
	return out, nil
}

// LatestEdition returns the latest stored paper without making any model calls.
func (s *Service) LatestEdition(ctx context.Context, a Actor) (Edition, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return Edition{}, err
	}
	out, err := s.editionByID(ctx, a.UserID, 0)
	if err != nil {
		return out, err
	}
	if !pipelineEngine(ctx).Enabled(llm.TaskEdition) {
		out.Status = "list"
		out.Model = ""
		for i := range out.Sections {
			out.Sections[i].Claims = []pipeline.GroundedClaim{}
			out.Sections[i].Sources = []pipeline.Source{}
			for j := range out.Sections[i].Items {
				out.Sections[i].Items[j].Summary = nil
				out.Sections[i].Items[j].Blurb = nil
			}
		}
	}
	return out, nil
}
