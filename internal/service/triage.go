package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const revertHandleLifetime = 24 * time.Hour

type TriageAction string

const (
	TriageSeen TriageAction = "seen"
	TriageDone TriageAction = "done"
)

type TriageRequest struct {
	OpID       string
	Generation string
	EntryIDs   []int64
	Action     TriageAction
}
type TriageResult struct {
	Results      []syncmodel.Result `json:"results"`
	RevertHandle string             `json:"revert_handle"`
	ExpiresAt    time.Time          `json:"expires_at"`
}

// Triage changes human state with agent provenance and returns an expiring handle.
func (s *Service) Triage(ctx context.Context, a Actor, r TriageRequest) (TriageResult, error) {
	if a.Kind != PrincipalAgent {
		return TriageResult{}, ErrForbidden
	}
	if err := s.checkActor(ctx, a, ScopeStateWriteHuman); err != nil {
		return TriageResult{}, err
	}
	if !validOpID(r.OpID) || len(r.OpID) > 128 || (r.Action != TriageSeen && r.Action != TriageDone) {
		return TriageResult{}, ErrInvalid
	}
	ids, err := canonicalEntries(r.EntryIDs)
	if err != nil {
		return TriageResult{}, err
	}
	var out TriageResult
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if current.Generation != r.Generation {
			return ErrGone
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "Triage", r.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		var human int64
		if err := tx.QueryRow(ctx, "SELECT id FROM principals WHERE user_id=$1 AND kind='human'", a.UserID).Scan(&human); err != nil {
			return err
		}
		var previous int64
		if err := tx.QueryRow(ctx, "SELECT coalesce(max(id),0) FROM activity WHERE user_id=$1", a.UserID).Scan(&previous); err != nil {
			return err
		}
		writes := make([]syncmodel.Write, len(ids))
		for i, id := range ids {
			register, value := syncmodel.Seen, syncmodel.SeenValue
			if r.Action == TriageDone {
				register, value = syncmodel.Shelf, syncmodel.Done
			}
			// A stale agent cannot outrank a deliberate human register.
			writes[i] = syncmodel.Write{OpID: fmt.Sprintf("%s/triage/%d", r.OpID, id), Principal: syncmodel.ID(human), EntryID: syncmodel.ID(id), Register: register, Value: value, Base: syncmodel.Revision{Generation: r.Generation}, OccurredAt: s.now(), Kind: syncmodel.Explicit}
		}
		out.Results, err = s.batchTx(ctx, tx, a, writes, current, s.now())
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT id FROM activity WHERE user_id=$1 AND principal_id=$2 AND id>$3 AND action='state.updated' ORDER BY id", a.UserID, a.PrincipalID, previous)
		if err != nil {
			return err
		}
		activityIDs := []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			activityIDs = append(activityIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return fmt.Errorf("revert handle randomness: %w", err)
		}
		out.RevertHandle = base64.RawURLEncoding.EncodeToString(token[:])
		out.ExpiresAt = s.now().Add(revertHandleLifetime)
		if _, err := tx.Exec(ctx, "INSERT INTO triage_handles(handle_hash,principal_id,generation,activity_ids,expires_at,op_id) VALUES($1,$2,$3,$4,$5,$6)", hashSecret(out.RevertHandle), a.PrincipalID, r.Generation, activityIDs, out.ExpiresAt, r.OpID); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, r.OpID, out)
	})
	return out, syncError(err)
}

// RevertTriage rechecks principal, scope, expiry and every revision before committing.
func (s *Service) RevertTriage(ctx context.Context, a Actor, handle, opID string) error {
	if err := s.stateActor(ctx, a); err != nil {
		return err
	}
	if !validOpID(opID) || len(opID) > 128 {
		return ErrInvalid
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		var principal int64
		var generation string
		var expires time.Time
		var ids []int64
		err := tx.QueryRow(ctx, "SELECT principal_id,generation::text,activity_ids,expires_at FROM triage_handles WHERE handle_hash=$1", hashSecret(handle)).Scan(&principal, &generation, &ids, &expires)
		if err != nil {
			return domainError(err)
		}
		if principal != a.PrincipalID {
			return ErrForbidden
		}
		if !s.now().Before(expires) {
			return fmt.Errorf("revert handle expired: %w", ErrGone)
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if current.Generation != generation {
			return ErrGone
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "RevertTriage", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		// Reverse order also supports handles containing successive changes to one entry.
		for i := len(ids) - 1; i >= 0; i-- {
			if err := s.revertTx(ctx, tx, a, ids[i], fmt.Sprintf("%s/activity/%d", opID, ids[i])); err != nil {
				return err
			}
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"reverted": len(ids)})
	})
}
