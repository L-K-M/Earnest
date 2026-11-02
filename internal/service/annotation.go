package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

type AnnotationKind string

const (
	AnnotationNote    AnnotationKind = "note"
	AnnotationLabel   AnnotationKind = "label"
	AnnotationScore   AnnotationKind = "score"
	AnnotationBrief   AnnotationKind = "brief"
	AnnotationSummary AnnotationKind = "summary"
	AnnotationBlurb   AnnotationKind = "blurb"
)
const (
	maxAnnotationEntries     = 1000
	maxNoteLength            = 8192
	maxBriefLength           = 16384
	maxAnnotationLabelLength = 128
	maxAnnotationScoreLength = 32
	maxBlurbLength           = 512
)

// Annotation is plain text owned by a principal, never a native rule label.
type Annotation struct {
	ID          int64          `json:"id"`
	PrincipalID int64          `json:"principal_id"`
	Kind        AnnotationKind `json:"kind"`
	EntryIDs    []int64        `json:"entry_ids"`
	Value       string         `json:"value"`
	Rev         int64          `json:"rev"`
	Untrusted   bool           `json:"untrusted"`
	Deleted     bool           `json:"deleted"`
}
type AnnotationQuery struct {
	IncludeDeleted bool
	PrincipalID    int64
	EntryIDs       []int64
	Kind           AnnotationKind
	AfterID        int64
	Limit          int
}
type PutAnnotationRequest struct {
	OpID     string
	EntryIDs []int64
	Kind     AnnotationKind
	Value    string
}

func plainText(value string, limit int) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("invalid UTF-8: %w", ErrInvalid)
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > limit {
		return "", fmt.Errorf("text must contain 1 to %d characters: %w", limit, ErrInvalid)
	}
	return value, nil
}
func annotationLimit(k AnnotationKind) int {
	switch k {
	case AnnotationNote, AnnotationSummary:
		return maxNoteLength
	case AnnotationLabel:
		return maxAnnotationLabelLength
	case AnnotationScore:
		return maxAnnotationScoreLength
	case AnnotationBrief:
		return maxBriefLength
	case AnnotationBlurb:
		return maxBlurbLength
	default:
		return 0
	}
}
func canonicalEntries(ids []int64) ([]int64, error) {
	if len(ids) == 0 || len(ids) > maxAnnotationEntries {
		return nil, ErrInvalid
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	for _, id := range out {
		if id <= 0 {
			return nil, ErrInvalid
		}
	}
	return slices.Compact(out), nil
}
func visibleEntries(ctx context.Context, tx pgx.Tx, user int64, ids []int64) error {
	for _, id := range ids {
		e, err := stateEntry(ctx, tx, user, syncmodel.ID(id))
		if err != nil {
			return err
		}
		if e.ID == 0 || e.Deleted {
			return fmt.Errorf("entry %d: %w", id, ErrNotFound)
		}
	}
	return nil
}

// PutAnnotation upserts the author's annotation for a canonical entry set and kind.
func (s *Service) PutAnnotation(ctx context.Context, a Actor, r PutAnnotationRequest) (Annotation, error) {
	if err := s.checkActor(ctx, a, ScopeAnnotationsWrite); err != nil {
		return Annotation{}, err
	}
	if (r.Kind == AnnotationSummary || r.Kind == AnnotationBlurb) && (a.Kind != PrincipalPipeline || a.Via != CredentialInternal) {
		return Annotation{}, ErrForbidden
	}
	limit := annotationLimit(r.Kind)
	if limit == 0 || !validOpID(r.OpID) {
		return Annotation{}, ErrInvalid
	}
	text, err := plainText(r.Value, limit)
	if err != nil {
		return Annotation{}, err
	}
	ids, err := canonicalEntries(r.EntryIDs)
	if err != nil {
		return Annotation{}, err
	}
	var out Annotation
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "PutAnnotation", r.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		if err := visibleEntries(ctx, tx, a.UserID, ids); err != nil {
			return err
		}
		var before *Annotation
		old, err := annotationByKey(ctx, tx, a.UserID, a.PrincipalID, ids, r.Kind)
		if err == nil {
			before = &old
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out = Annotation{PrincipalID: a.PrincipalID, Kind: r.Kind, EntryIDs: ids, Value: text}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		out.Rev = rev
		if before == nil {
			err = tx.QueryRow(ctx, "INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id", a.UserID, a.PrincipalID, r.Kind, text, ids, rev, s.now()).Scan(&out.ID)
		} else {
			out.ID = before.ID
			_, err = tx.Exec(ctx, "UPDATE annotations SET value=$2,rev=$3 WHERE id=$1", out.ID, text, rev)
		}
		if err != nil {
			return fmt.Errorf("put annotation: %w", err)
		}
		if err := store.InsertChange(ctx, tx, a.UserID, rev, string(syncmodel.AnnotationUpdated), annotationPayload(out), s.now()); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, tx, a, "annotation.updated", map[string]any{"id": out.ID}, before, out); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, r.OpID, out)
	})
	return out, domainError(err)
}
func annotationByKey(ctx context.Context, tx pgx.Tx, user, principal int64, ids []int64, kind AnnotationKind) (Annotation, error) {
	var out Annotation
	err := tx.QueryRow(ctx, "SELECT id,principal_id,kind,entry_ids,value,rev FROM annotations WHERE user_id=$1 AND principal_id=$2 AND entry_ids=$3 AND kind=$4 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1", user, principal, ids, kind).Scan(&out.ID, &out.PrincipalID, &out.Kind, &out.EntryIDs, &out.Value, &out.Rev)
	return out, err
}

// GetAnnotation reads by author, canonical entry set and kind.
func (s *Service) GetAnnotation(ctx context.Context, a Actor, principal int64, ids []int64, kind AnnotationKind) (Annotation, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return Annotation{}, err
	}
	ids, err := canonicalEntries(ids)
	if err != nil {
		return Annotation{}, err
	}
	var out Annotation
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = annotationByKey(ctx, tx, a.UserID, principal, ids, kind)
		if err != nil {
			return err
		}
		return markAnnotationTrust(ctx, tx, a, &out)
	})
	return out, domainError(err)
}
func markAnnotationTrust(ctx context.Context, tx pgx.Tx, a Actor, out *Annotation) error {
	if a.Kind != PrincipalAgent || out.PrincipalID == a.PrincipalID {
		return nil
	}
	return tx.QueryRow(ctx, "SELECT true FROM principals WHERE id=$1", out.PrincipalID).Scan(&out.Untrusted)
}

// ListAnnotations uses stable IDs and a bounded page; entry filtering matches any member.
func (s *Service) ListAnnotations(ctx context.Context, a Actor, q AnnotationQuery) ([]Annotation, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 1000 || q.AfterID < 0 || len(q.EntryIDs) > maxAnnotationEntries {
		return nil, ErrInvalid
	}
	out := []Annotation{}
	rows, err := s.store.Pool().Query(ctx, `SELECT n.id,n.principal_id,n.kind,n.entry_ids,n.value,n.rev,n.principal_id<>$2 AND $3,n.deleted_at IS NOT NULL
 FROM annotations n JOIN principals p ON p.id=n.principal_id WHERE n.user_id=$1 AND ($9 OR n.deleted_at IS NULL) AND n.id>$4
 AND ($5::bigint=0 OR n.principal_id=$5) AND ($6::text='' OR n.kind=$6) AND (cardinality($7::bigint[])=0 OR n.entry_ids && $7)
 ORDER BY n.id LIMIT $8`, a.UserID, a.PrincipalID, a.Kind == PrincipalAgent, q.AfterID, q.PrincipalID, q.Kind, nonNilIDs(q.EntryIDs), q.Limit, q.IncludeDeleted)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n Annotation
		if err := rows.Scan(&n.ID, &n.PrincipalID, &n.Kind, &n.EntryIDs, &n.Value, &n.Rev, &n.Untrusted, &n.Deleted); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func nonNilIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// DeleteAnnotation removes only the caller's annotation and retains a revisioned tombstone.
func (s *Service) DeleteAnnotation(ctx context.Context, a Actor, ids []int64, kind AnnotationKind, opID string) error {
	if err := s.checkActor(ctx, a, ScopeAnnotationsWrite); err != nil {
		return err
	}
	if !validOpID(opID) {
		return ErrInvalid
	}
	ids, err := canonicalEntries(ids)
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "DeleteAnnotation", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		old, err := annotationByKey(ctx, tx, a.UserID, a.PrincipalID, ids, kind)
		if err != nil {
			return domainError(err)
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE annotations SET deleted_at=$2,rev=$3 WHERE id=$1", old.ID, s.now(), rev); err != nil {
			return err
		}
		tombstone := old
		tombstone.Rev = rev
		tombstone.Deleted = true
		if err := store.InsertChange(ctx, tx, a.UserID, rev, string(syncmodel.AnnotationDeleted), annotationPayload(tombstone), s.now()); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, tx, a, "annotation.deleted", map[string]any{"id": old.ID, "rev": rev}, old, nil); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"id": old.ID})
	})
}

// TodayBriefs returns the newest live brief from each agent.
func (s *Service) TodayBriefs(ctx context.Context, a Actor) ([]Annotation, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT DISTINCT ON(n.principal_id) n.id,n.principal_id,n.kind,n.entry_ids,n.value,n.rev
 FROM annotations n JOIN principals p ON p.id=n.principal_id WHERE n.user_id=$1 AND p.kind='agent' AND n.kind='brief' AND n.deleted_at IS NULL
 ORDER BY n.principal_id,n.rev DESC,n.id DESC`, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Annotation{}
	for rows.Next() {
		var n Annotation
		if err := rows.Scan(&n.ID, &n.PrincipalID, &n.Kind, &n.EntryIDs, &n.Value, &n.Rev); err != nil {
			return nil, err
		}
		n.Untrusted = a.Kind == PrincipalAgent && a.PrincipalID != n.PrincipalID
		out = append(out, n)
	}
	return out, rows.Err()
}

// Shared change events cannot be tailored to the reader, so they conservatively
// mark annotation text untrusted for every consumer, including its author.
func annotationPayload(n Annotation) map[string]any {
	n.Untrusted = true
	object := syncmodel.Object{Kind: syncmodel.AnnotationObject, ID: syncmodel.ID(n.ID), Principal: syncmodel.ID(n.PrincipalID), Name: string(n.Kind), Rev: uint64(n.Rev), Deleted: n.Deleted}
	if len(n.EntryIDs) == 1 {
		object.EntryID = syncmodel.ID(n.EntryIDs[0])
	}
	return map[string]any{"annotation": n, "object": object, "id": n.ID, "rev": n.Rev, "deleted": n.Deleted}
}
