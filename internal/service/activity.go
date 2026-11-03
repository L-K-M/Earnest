package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

type ActivityNextAction string

const (
	ActivityRevert  ActivityNextAction = "revert"
	ActivityCancel  ActivityNextAction = "cancel"
	ActivityNothing ActivityNextAction = "nothing"
)

// Activity is immutable provenance. NextAction is computed from current state.
type Activity struct {
	ID            int64              `json:"id"`
	PrincipalID   int64              `json:"principal_id"`
	PrincipalName string             `json:"principal_name"`
	Action        string             `json:"action"`
	Target        json.RawMessage    `json:"target"`
	Before        json.RawMessage    `json:"before"`
	After         json.RawMessage    `json:"after"`
	Generation    string             `json:"generation"`
	CreatedAt     time.Time          `json:"created_at"`
	NextAction    ActivityNextAction `json:"next_action"`
	Reason        string             `json:"reason,omitempty"`
}
type ActivityFilter struct {
	PrincipalID int64
	Action      string
	Since       time.Time
	AfterID     int64
	Limit       int
}
type ActivityGroup struct {
	PrincipalID   int64
	PrincipalName string
	Activities    []Activity
}

func (s *Service) recordActivity(ctx context.Context, tx pgx.Tx, a Actor, action string, target, before, after any) error {
	if a.Kind == PrincipalHuman {
		return nil
	}
	t, err := json.Marshal(target)
	if err != nil {
		return err
	}
	b, err := json.Marshal(before)
	if err != nil {
		return err
	}
	n, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO activity(user_id,principal_id,action,target,before,after,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", a.UserID, a.PrincipalID, action, t, b, n, s.now())
	if err != nil {
		return fmt.Errorf("record activity: %w", err)
	}
	return nil
}
func (s *Service) loadActivity(ctx context.Context, tx pgx.Tx, a Actor, id int64) (Activity, error) {
	var out Activity
	err := tx.QueryRow(ctx, `SELECT a.id,a.principal_id,p.name,a.action,a.target,a.before,a.after,a.generation::text,a.created_at
 FROM activity a JOIN principals p ON p.id=a.principal_id WHERE a.user_id=$1 AND a.id=$2`, a.UserID, id).Scan(&out.ID, &out.PrincipalID, &out.PrincipalName, &out.Action, &out.Target, &out.Before, &out.After, &out.Generation, &out.CreatedAt)
	if err != nil {
		return out, domainError(err)
	}
	if a.Kind != PrincipalHuman && out.PrincipalID != a.PrincipalID {
		return out, ErrForbidden
	}
	return out, nil
}
func (s *Service) activityNext(ctx context.Context, tx pgx.Tx, a Actor, row *Activity) error {
	row.NextAction = ActivityNothing
	var done bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM activity_reverts WHERE activity_id=$1)", row.ID).Scan(&done); err != nil {
		return err
	}
	if done {
		row.Reason = "already reverted"
		return nil
	}
	current, err := stateRevision(ctx, tx, a.UserID)
	if err != nil {
		return err
	}
	if row.Generation != current.Generation {
		row.Reason = "database generation changed"
		return nil
	}
	switch row.Action {
	case "state.updated":
		var after syncmodel.State
		if err := json.Unmarshal(row.After, &after); err != nil {
			return err
		}
		var before syncmodel.State
		if err := json.Unmarshal(row.Before, &before); err != nil {
			return err
		}
		state, err := store.LoadState(ctx, tx, int64(after.Principal), int64(after.EntryID))
		if err != nil {
			return err
		}
		for _, name := range []syncmodel.RegisterName{syncmodel.Seen, syncmodel.Shelf, syncmodel.Kept} {
			b, _ := before.Register(name)
			n, _ := after.Register(name)
			c, _ := state.Register(name)
			if b.Rev != n.Rev && c.Rev != n.Rev {
				row.Reason = fmt.Sprintf("%s changed after this action", name)
				return nil
			}
		}
		row.NextAction = ActivityRevert
	case "annotation.updated", "annotation.deleted":
		var target struct {
			ID  int64
			Rev int64
		}
		if err := json.Unmarshal(row.Target, &target); err != nil {
			return err
		}
		var rev int64
		err := tx.QueryRow(ctx, "SELECT rev FROM annotations WHERE id=$1 AND user_id=$2", target.ID, a.UserID).Scan(&rev)
		if errors.Is(err, pgx.ErrNoRows) {
			row.Reason = "annotation no longer exists"
			return nil
		}
		if err != nil {
			return err
		}
		if target.Rev == 0 {
			var after Annotation
			if err := json.Unmarshal(row.After, &after); err != nil {
				return err
			}
			target.Rev = after.Rev
		}
		if rev != target.Rev {
			row.Reason = "annotation changed after this action"
			return nil
		}
		row.NextAction = ActivityRevert
	case "position.updated":
		var after struct {
			Principal int64 `json:"principal_id"`
			View      string
			Rev       uint64
		}
		if err := json.Unmarshal(row.After, &after); err != nil {
			return err
		}
		var rev uint64
		if err := tx.QueryRow(ctx, "SELECT rev FROM positions WHERE principal_id=$1 AND view=$2", after.Principal, after.View).Scan(&rev); err != nil {
			return domainError(err)
		}
		if rev != after.Rev {
			row.Reason = "position changed after this action"
			return nil
		}
		row.NextAction = ActivityRevert
	case "notification.created":
		var target struct{ ID int64 }
		if err := json.Unmarshal(row.Target, &target); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM notifications WHERE id=$1 AND recipient_user_id=$2", target.ID, a.UserID).Scan(&status); err != nil {
			return domainError(err)
		}
		if status == "held" {
			row.NextAction = ActivityCancel
		} else {
			row.Reason = "notification is " + status
		}
	}
	return nil
}

// ListActivity limits agents to their own provenance; humans can filter all principals.
func (s *Service) ListActivity(ctx context.Context, a Actor, f ActivityFilter) ([]Activity, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if a.Kind != PrincipalHuman {
		if f.PrincipalID != 0 && f.PrincipalID != a.PrincipalID {
			return nil, ErrForbidden
		}
		f.PrincipalID = a.PrincipalID
	}
	if f.Limit == 0 {
		f.Limit = 100
	}
	if f.Limit < 1 || f.Limit > 1000 || f.AfterID < 0 {
		return nil, ErrInvalid
	}
	out := []Activity{}
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT a.id,a.principal_id,p.name,a.action,a.target,a.before,a.after,a.generation::text,a.created_at
 FROM activity a JOIN principals p ON p.id=a.principal_id WHERE a.user_id=$1 AND ($2::bigint=0 OR a.principal_id=$2)
 AND ($3::text='' OR a.action=$3) AND a.created_at>=$4 AND a.id>$5 ORDER BY a.id LIMIT $6`, a.UserID, f.PrincipalID, f.Action, f.Since, f.AfterID, f.Limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r Activity
			if err := rows.Scan(&r.ID, &r.PrincipalID, &r.PrincipalName, &r.Action, &r.Target, &r.Before, &r.After, &r.Generation, &r.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out = append(out, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out {
			if err := s.activityNext(ctx, tx, a, &out[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// ActivityByPrincipal groups a bounded activity page for a while-away view.
func (s *Service) ActivityByPrincipal(ctx context.Context, a Actor, f ActivityFilter) ([]ActivityGroup, error) {
	rows, err := s.ListActivity(ctx, a, f)
	if err != nil {
		return nil, err
	}
	out := []ActivityGroup{}
	index := map[int64]int{}
	for _, r := range rows {
		i, ok := index[r.PrincipalID]
		if !ok {
			i = len(out)
			index[r.PrincipalID] = i
			out = append(out, ActivityGroup{PrincipalID: r.PrincipalID, PrincipalName: r.PrincipalName})
		}
		out[i].Activities = append(out[i].Activities, r)
	}
	return out, nil
}

// Revert uses compare-and-set revisions and the ordinary state resolver, atomically.
func (s *Service) Revert(ctx context.Context, a Actor, id int64, opID string) error {
	if err := s.revertActor(ctx, a); err != nil {
		return err
	}
	if !validOpID(opID) {
		return ErrInvalid
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		return s.revertTx(ctx, tx, a, id, opID)
	})
}
func (s *Service) revertActor(ctx context.Context, a Actor) error {
	for _, scope := range []Scope{ScopeStateWrite, ScopeStateWriteHuman, ScopeAnnotationsWrite, ScopePositionOwn} {
		if a.Has(scope) {
			return s.checkActor(ctx, a, scope)
		}
	}
	return ErrForbidden
}

func (s *Service) revertTx(ctx context.Context, tx pgx.Tx, a Actor, id int64, opID string) error {
	replay, err := replayOp(ctx, tx, a.PrincipalID, "Revert", opID)
	if err != nil {
		return err
	}
	if replay != nil {
		return nil
	}
	row, err := s.loadActivity(ctx, tx, a, id)
	if err != nil {
		return err
	}
	if row.PrincipalID != a.PrincipalID {
		var kind PrincipalKind
		if err := tx.QueryRow(ctx, "SELECT kind FROM principals WHERE id=$1", row.PrincipalID).Scan(&kind); err != nil {
			return err
		}
		if a.Kind != PrincipalHuman || kind != PrincipalAgent {
			return ErrForbidden
		}
	}
	if err := s.activityNext(ctx, tx, a, &row); err != nil {
		return err
	}
	if row.NextAction != ActivityRevert {
		return fmt.Errorf("cannot revert: %s: %w", row.Reason, ErrConflict)
	}
	switch row.Action {
	case "state.updated":
		if err := s.stateActor(ctx, a); err != nil {
			return err
		}
		var before, after syncmodel.State
		if err := json.Unmarshal(row.Before, &before); err != nil {
			return err
		}
		if err := json.Unmarshal(row.After, &after); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		writes := []syncmodel.Write{}
		for _, name := range []syncmodel.RegisterName{syncmodel.Seen, syncmodel.Shelf, syncmodel.Kept} {
			b, _ := before.Register(name)
			n, _ := after.Register(name)
			if b.Rev == n.Rev {
				continue
			}
			writes = append(writes, syncmodel.Write{OpID: fmt.Sprintf("%s/%s", opID, name), Principal: before.Principal, EntryID: before.EntryID, Register: name, Value: b.Value, Base: syncmodel.Revision{Generation: current.Generation, Rev: n.Rev}, OccurredAt: s.now(), Kind: syncmodel.Explicit})
		}
		// Reverting one's own agent action on human state remains authorized by its scope.
		if a.Kind == PrincipalHuman && int64(before.Principal) != a.PrincipalID {
			if err := s.revertOwnedStateTx(ctx, tx, a, writes, current); err != nil {
				return err
			}
		} else if _, err := s.batchTx(ctx, tx, a, writes, current, s.now()); err != nil {
			return syncError(err)
		}
	case "annotation.updated", "annotation.deleted":
		if err := s.checkActor(ctx, a, ScopeAnnotationsWrite); err != nil {
			return err
		}
		var target struct{ ID int64 }
		if err := json.Unmarshal(row.Target, &target); err != nil {
			return err
		}
		var before *Annotation
		if err := json.Unmarshal(row.Before, &before); err != nil {
			return err
		}
		kind := syncmodel.AnnotationUpdated
		if before == nil {
			kind = syncmodel.AnnotationDeleted
		}
		rev, err := s.change(ctx, tx, a.UserID, kind, map[string]any{"id": target.ID})
		if err != nil {
			return err
		}
		if before == nil {
			_, err = tx.Exec(ctx, "UPDATE annotations SET deleted_at=$2,rev=$3 WHERE id=$1", target.ID, s.now(), rev)
		} else {
			_, err = tx.Exec(ctx, "UPDATE annotations SET value=$2,deleted_at=NULL,rev=$3 WHERE id=$1", target.ID, before.Value, rev)
		}
		if err != nil {
			return err
		}
		var restored Annotation
		var deleted bool
		if err := tx.QueryRow(ctx, "SELECT id,principal_id,kind,entry_ids,value,rev,deleted_at IS NOT NULL FROM annotations WHERE id=$1", target.ID).Scan(&restored.ID, &restored.PrincipalID, &restored.Kind, &restored.EntryIDs, &restored.Value, &restored.Rev, &deleted); err != nil {
			return err
		}
		restored.Deleted = deleted
		payload, err := json.Marshal(annotationPayload(restored))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE changes SET payload=$3 WHERE user_id=$1 AND rev=$2", a.UserID, rev, payload); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, tx, a, "annotation.reverted", map[string]any{"id": target.ID}, json.RawMessage(row.After), before); err != nil {
			return err
		}
	case "position.updated":
		scope := ScopeStateWrite
		if a.Kind == PrincipalAgent {
			scope = ScopePositionOwn
		}
		if err := s.checkActor(ctx, a, scope); err != nil {
			return err
		}
		var target struct {
			Principal int64
			View      string
		}
		if err := json.Unmarshal(row.Target, &target); err != nil {
			return err
		}
		var before struct {
			Anchor uint64 `json:"anchor_seq"`
		}
		if string(row.Before) != "null" && len(row.Before) > 0 {
			if err := json.Unmarshal(row.Before, &before); err != nil {
				return err
			}
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('earnest.position_actor',$1,true)", fmt.Sprint(a.PrincipalID)); err != nil {
			return err
		}
		if err := store.UpsertPosition(ctx, tx, target.Principal, target.View, before.Anchor, rev); err != nil {
			return err
		}
		position := syncmodel.Position{Principal: syncmodel.ID(target.Principal), View: target.View, Seq: before.Anchor, Rev: uint64(rev)}
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.PositionUpdated, "position", position); err != nil {
			return err
		}
	default:
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, "INSERT INTO activity_reverts(activity_id,reverted_by,created_at) VALUES($1,$2,$3)", id, a.PrincipalID, s.now()); err != nil {
		return err
	}
	return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"id": id})
}

// AgentChangesToUnseen reports changes whose human seen register was unseen before the action.
func (s *Service) AgentChangesToUnseen(ctx context.Context, a Actor, since time.Time) ([]Activity, error) {
	if a.Kind != PrincipalHuman {
		return nil, ErrForbidden
	}
	rows, err := s.ListActivity(ctx, a, ActivityFilter{Action: "state.updated", Since: since, Limit: 1000})
	if err != nil {
		return nil, err
	}
	out := []Activity{}
	for _, r := range rows {
		var before syncmodel.State
		if err := json.Unmarshal(r.Before, &before); err != nil {
			return nil, err
		}
		if int64(before.Principal) == a.PrincipalID && before.Seen.Value == syncmodel.Unseen {
			var agent bool
			if err := s.store.Pool().QueryRow(ctx, "SELECT kind='agent' FROM principals WHERE id=$1", r.PrincipalID).Scan(&agent); err != nil {
				return nil, err
			}
			if agent {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// Human undo of an agent's own register uses the same resolver after ownership
// was checked against the immutable activity, rather than widening stateTarget.
func (s *Service) revertOwnedStateTx(ctx context.Context, tx pgx.Tx, a Actor, writes []syncmodel.Write, current syncmodel.Revision) error {
	next := syncmodel.Revision{Generation: current.Generation, Rev: current.Rev + 1}
	trusted := syncmodel.Context{Actor: syncmodel.ID(a.PrincipalID), Token: a.SyncTokenKind(), Receipt: s.now()}
	results := []syncmodel.Result{}
	for _, w := range writes {
		old, err := loadStateOperation(ctx, tx, a.PrincipalID, w.OpID)
		if err != nil {
			return err
		}
		if old != nil {
			return ErrInvalid
		}
		state, err := store.LoadState(ctx, tx, int64(w.Principal), int64(w.EntryID))
		if err != nil {
			return err
		}
		entry, err := stateEntry(ctx, tx, a.UserID, w.EntryID)
		if err != nil {
			return err
		}
		r, err := syncmodel.ResolveWrite(trusted, w, entry, state, next)
		if err != nil {
			return syncError(err)
		}
		if r.Status != syncmodel.Applied {
			return fmt.Errorf("revert target no longer applicable: %w", ErrConflict)
		}
		if err := store.SaveState(ctx, tx, r.State); err != nil {
			return err
		}
		results = append(results, r)
	}
	for i := range results {
		state, err := store.LoadState(ctx, tx, int64(results[i].State.Principal), int64(results[i].State.EntryID))
		if err != nil {
			return err
		}
		results[i].State = state
		if err := saveStateOperation(ctx, tx, a.PrincipalID, writes[i].OpID, stateOperation{Kind: "state", Result: &results[i]}, s.now()); err != nil {
			return err
		}
	}
	_, err := s.change(ctx, tx, a.UserID, syncmodel.StateUpdated, map[string]any{"results": results})
	return err
}
