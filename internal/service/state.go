package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const maxStateBatch = 10000

type stateKey struct{ principal, entry syncmodel.ID }
type stateOperation struct {
	Kind    string                 `json:"kind"`
	Result  *syncmodel.Result      `json:"result,omitempty"`
	Range   *syncmodel.RangeResult `json:"range,omitempty"`
	Results []syncmodel.Result     `json:"results,omitempty"`
}

// ThroughRequest fixes the inclusive boundary explicitly, by receipt time, or by ts.
// Omit ThroughSeq and At to use the highest committed seq at receipt.
type ThroughRequest struct {
	Write      syncmodel.Write `json:"write"`
	Scope      syncmodel.Scope `json:"scope"`
	ThroughSeq *uint64         `json:"through_seq,omitempty"`
	At         *time.Time      `json:"at,omitempty"`
}

func syncError(err error) error {
	if errors.Is(err, syncmodel.ErrGeneration) {
		return fmt.Errorf("%w: %w", ErrGone, err)
	}
	if errors.Is(err, syncmodel.ErrInvalid) {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return err
}

func (s *Service) stateActor(ctx context.Context, a Actor) error {
	scope := ScopeStateWrite
	if a.Kind == PrincipalAgent {
		scope = ScopeStateWriteHuman
	}
	return s.checkActor(ctx, a, scope)
}

func stateRevision(ctx context.Context, tx pgx.Tx, user int64) (syncmodel.Revision, error) {
	var r syncmodel.Revision
	err := tx.QueryRow(ctx, `SELECT generation::text,coalesce((SELECT value FROM rev_counters WHERE user_id=$1),0) FROM system WHERE singleton`, user).Scan(&r.Generation, &r.Rev)
	if err != nil {
		return r, fmt.Errorf("read state revision: %w", err)
	}
	return r, nil
}

func (s *Service) stateTarget(ctx context.Context, tx pgx.Tx, a Actor, principal syncmodel.ID) error {
	var kind PrincipalKind
	err := tx.QueryRow(ctx, "SELECT kind FROM principals WHERE id=$1 AND user_id=$2", int64(principal), a.UserID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("state target: %w", err)
	}
	if int64(principal) == a.PrincipalID {
		return nil
	}
	if a.Kind == PrincipalAgent && kind == PrincipalHuman && a.Has(ScopeStateWriteHuman) {
		return nil
	}
	if a.Kind == PrincipalPipeline && a.Via == CredentialInternal && kind == PrincipalHuman && a.Has(ScopeStateWrite) {
		return nil
	}
	return ErrForbidden
}

// stateEntry checks visibility through an active subscription, including private
// feeds. Missing/unsubscribed bodies are represented as Gone by the model.
func stateEntry(ctx context.Context, tx pgx.Tx, user int64, id syncmodel.ID) (syncmodel.Entry, error) {
	var e syncmodel.Entry
	var deleted bool
	err := tx.QueryRow(ctx, `SELECT e.id,s.id,e.seq,e.first_seen_at,e.deleted_at IS NOT NULL
 FROM entries e JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL
 WHERE e.id=$2`, user, int64(id)).Scan(&e.ID, &e.SubscriptionID, &e.Seq, &e.FirstSeenAt, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, nil
	}
	if err != nil {
		return e, fmt.Errorf("state entry visibility: %w", err)
	}
	e.Deleted = deleted
	return e, nil
}

func loadStateOperation(ctx context.Context, tx pgx.Tx, actor int64, id string) (*stateOperation, error) {
	var body []byte
	err := tx.QueryRow(ctx, "SELECT result FROM op_ids WHERE principal_id=$1 AND op_id=$2", actor, id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state deduplication: %w", err)
	}
	var op stateOperation
	if err := json.Unmarshal(body, &op); err != nil {
		return nil, fmt.Errorf("decode state operation: %w", err)
	}
	return &op, nil
}
func saveStateOperation(ctx context.Context, tx pgx.Tx, actor int64, id string, op stateOperation, at time.Time) error {
	body, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("encode state operation: %w", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO op_ids(principal_id,op_id,result,created_at) VALUES($1,$2,$3,$4)", actor, id, body, at); err != nil {
		return fmt.Errorf("remember state operation: %w", err)
	}
	return nil
}

func replayState(ctx context.Context, tx pgx.Tx, user int64, original syncmodel.Result) (syncmodel.Result, error) {
	original.Replay = true
	state, err := store.LoadState(ctx, tx, int64(original.State.Principal), int64(original.State.EntryID))
	if err != nil {
		return original, err
	}
	original.State = state
	entry, err := stateEntry(ctx, tx, user, state.EntryID)
	if err != nil {
		return original, err
	}
	if entry.ID == 0 || entry.Deleted {
		original.Status = syncmodel.Gone
	}
	return original, nil
}

// Batch validates all writes before committing any, and emits one compact event.
// Principal is the state owner, not necessarily the authenticated actor.
func (s *Service) Batch(ctx context.Context, a Actor, writes []syncmodel.Write) ([]syncmodel.Result, error) {
	if err := s.stateActor(ctx, a); err != nil {
		return nil, err
	}
	if len(writes) > maxStateBatch {
		return nil, fmt.Errorf("batch exceeds %d: %w", maxStateBatch, ErrInvalid)
	}
	receipt := s.now().UTC()
	results := []syncmodel.Result{}
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		results, err = s.batchTx(ctx, tx, a, writes, current, receipt)
		return err
	})
	if err != nil {
		return nil, syncError(err)
	}
	return results, nil
}

func (s *Service) batchTx(ctx context.Context, tx pgx.Tx, a Actor, writes []syncmodel.Write, current syncmodel.Revision, receipt time.Time) ([]syncmodel.Result, error) {
	next := syncmodel.Revision{Generation: current.Generation, Rev: current.Rev + 1}
	trusted := syncmodel.Context{Actor: syncmodel.ID(a.PrincipalID), Token: a.SyncTokenKind(), Receipt: receipt}
	type target struct {
		stateKey
		register syncmodel.RegisterName
	}
	targets := map[target]bool{}
	ids := map[string]bool{}
	old := make([]*stateOperation, len(writes))
	// Resolve against a missing entry to invoke the model's full input validation
	// without making a decision about a live state or changing any database row.
	for i, w := range writes {
		if !validOpID(w.OpID) {
			return nil, fmt.Errorf("batch operation ID: %w", ErrInvalid)
		}
		if err := s.stateTarget(ctx, tx, a, w.Principal); err != nil {
			return nil, err
		}
		if _, err := syncmodel.ResolveWrite(trusted, w, syncmodel.Entry{}, syncmodel.InitialState(w.Principal, w.EntryID), next); err != nil {
			return nil, fmt.Errorf("batch item %d: %w", i, err)
		}
		t := target{stateKey{w.Principal, w.EntryID}, w.Register}
		if targets[t] || ids[w.OpID] {
			return nil, fmt.Errorf("duplicate target or operation: %w", ErrInvalid)
		}
		targets[t] = true
		ids[w.OpID] = true
		op, err := loadStateOperation(ctx, tx, a.PrincipalID, w.OpID)
		if err != nil {
			return nil, err
		}
		if op != nil && (op.Kind != "state" || op.Result == nil) {
			return nil, fmt.Errorf("operation belongs to another mutation: %w", ErrInvalid)
		}
		old[i] = op
	}
	states := map[stateKey]syncmodel.State{}
	results := make([]syncmodel.Result, 0, len(writes))
	applied := false
	for i, w := range writes {
		if old[i] != nil {
			r, err := replayState(ctx, tx, a.UserID, *old[i].Result)
			if err != nil {
				return nil, err
			}
			results = append(results, r)
			continue
		}
		key := stateKey{w.Principal, w.EntryID}
		state, ok := states[key]
		if !ok {
			var err error
			state, err = store.LoadState(ctx, tx, int64(w.Principal), int64(w.EntryID))
			if err != nil {
				return nil, err
			}
		}
		entry, err := stateEntry(ctx, tx, a.UserID, w.EntryID)
		if err != nil {
			return nil, err
		}
		r, err := syncmodel.ResolveWrite(trusted, w, entry, state, next)
		if err != nil {
			return nil, err
		}
		if r.Status == syncmodel.Applied {
			if err := store.SaveState(ctx, tx, r.State); err != nil {
				return nil, err
			}
			if err := s.stateActivity(ctx, tx, a, w, state, r.State, receipt); err != nil {
				return nil, err
			}
			applied = true
		}
		states[key] = r.State
		results = append(results, r)
	}
	// Return each target's final state, including Done clearing and later register writes.
	for i := range results {
		r := &results[i]
		state, err := store.LoadState(ctx, tx, int64(r.State.Principal), int64(r.State.EntryID))
		if err != nil {
			return nil, err
		}
		r.State = state
		if old[i] == nil {
			if err := saveStateOperation(ctx, tx, a.PrincipalID, writes[i].OpID, stateOperation{Kind: "state", Result: r}, receipt); err != nil {
				return nil, err
			}
		}
	}
	if applied {
		rev, err := s.change(ctx, tx, a.UserID, syncmodel.StateUpdated, map[string]any{"results": results})
		if err != nil {
			return nil, err
		}
		if uint64(rev) != next.Rev {
			return nil, fmt.Errorf("state revision changed within locked transaction: %w", ErrConflict)
		}
	}
	return results, nil
}

func (s *Service) stateActivity(ctx context.Context, tx pgx.Tx, a Actor, w syncmodel.Write, before, after syncmodel.State, at time.Time) error {
	if a.Kind != PrincipalAgent || int64(w.Principal) == a.PrincipalID {
		return nil
	}
	target, err := json.Marshal(map[string]any{"principal": w.Principal, "entry_id": w.EntryID, "register": w.Register, "op_id": w.OpID})
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
	_, err = tx.Exec(ctx, `INSERT INTO activity(user_id,principal_id,action,target,before,after,revert_state,created_at) VALUES($1,$2,'state.updated',$3,$4,$5,$5,$6)`, a.UserID, a.PrincipalID, target, b, n, at)
	if err != nil {
		return fmt.Errorf("record agent activity: %w", err)
	}
	return nil
}

// Through commits a resolved, inclusive range and its exception list atomically.
func (s *Service) Through(ctx context.Context, a Actor, request ThroughRequest) (syncmodel.RangeResult, error) {
	if err := s.stateActor(ctx, a); err != nil {
		return syncmodel.RangeResult{}, err
	}
	if request.ThroughSeq != nil && request.At != nil {
		return syncmodel.RangeResult{}, ErrInvalid
	}
	if request.ThroughSeq != nil && *request.ThroughSeq > math.MaxInt64 {
		return syncmodel.RangeResult{}, ErrInvalid
	}
	receipt := s.now().UTC()
	// A plain MVCC read excludes ingest still holding the mutation lock.
	var receiptSeq uint64
	if request.ThroughSeq == nil && request.At == nil {
		if err := s.store.Pool().QueryRow(ctx, "SELECT value FROM seq_counter WHERE singleton").Scan(&receiptSeq); err != nil {
			return syncmodel.RangeResult{}, fmt.Errorf("receipt range boundary: %w", err)
		}
	}
	var result syncmodel.RangeResult
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		next := syncmodel.Revision{Generation: current.Generation, Rev: current.Rev + 1}
		trusted := syncmodel.Context{Actor: syncmodel.ID(a.PrincipalID), Token: a.SyncTokenKind(), Receipt: receipt}
		w := request.Write
		w.EntryID = 1
		if err := s.stateTarget(ctx, tx, a, w.Principal); err != nil {
			return err
		}
		if _, err := syncmodel.ResolveWrite(trusted, w, syncmodel.Entry{}, syncmodel.InitialState(w.Principal, 1), next); err != nil {
			return err
		}
		if w.Register != syncmodel.Seen || w.Value != syncmodel.SeenValue {
			return ErrInvalid
		}
		if !validOpID(w.OpID) {
			return ErrInvalid
		}
		if err := validateStateScope(request.Scope); err != nil {
			return err
		}
		old, err := loadStateOperation(ctx, tx, a.PrincipalID, w.OpID)
		if err != nil {
			return err
		}
		if old != nil {
			if old.Kind != "range" || old.Range == nil {
				return ErrInvalid
			}
			result = *old.Range
			result.Replay = true
			for i, r := range result.Results {
				result.Results[i], err = replayState(ctx, tx, a.UserID, r)
				if err != nil {
					return err
				}
			}
			return nil
		}
		subs, err := resolveStateScope(ctx, tx, a.UserID, request.Scope)
		if err != nil {
			return err
		}
		var through uint64
		switch {
		case request.ThroughSeq != nil:
			through = *request.ThroughSeq
		case request.At != nil:
			err = tx.QueryRow(ctx, `SELECT coalesce(max(e.seq),0) FROM entries e JOIN subscriptions s ON e.feed_id=s.feed_id WHERE s.user_id=$1 AND s.deleted_at IS NULL AND e.first_seen_at<=$2`, a.UserID, *request.At).Scan(&through)
		default:
			through = receiptSeq
		}
		if err != nil {
			return fmt.Errorf("range boundary: %w", err)
		}
		result = syncmodel.RangeResult{Status: syncmodel.Applied, ThroughSeq: through, Subscriptions: subs, Except: []syncmodel.ID{}, Results: []syncmodel.Result{}}
		if receipt.Sub(syncmodel.OccurredAt(w.OccurredAt, receipt)) > syncmodel.MaxQueueAge {
			result.Status = syncmodel.Expired
			return saveStateOperation(ctx, tx, a.PrincipalID, w.OpID, stateOperation{Kind: "range", Range: &result}, receipt)
		}
		subIDs := make([]int64, len(subs))
		for i, id := range subs {
			subIDs[i] = int64(id)
		}
		rows, err := tx.Query(ctx, `SELECT e.id FROM entries e JOIN subscriptions s ON s.feed_id=e.feed_id WHERE s.user_id=$1 AND s.id=ANY($2) AND s.deleted_at IS NULL AND e.deleted_at IS NULL AND e.seq<=$3 ORDER BY e.id`, a.UserID, subIDs, int64(through))
		if err != nil {
			return fmt.Errorf("range members: %w", err)
		}
		members := []syncmodel.ID{}
		for rows.Next() {
			var id syncmodel.ID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			members = append(members, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range members {
			item := w
			item.EntryID = id
			state, err := store.LoadState(ctx, tx, int64(w.Principal), int64(id))
			if err != nil {
				return err
			}
			entry, err := stateEntry(ctx, tx, a.UserID, id)
			if err != nil {
				return err
			}
			r, err := syncmodel.ResolveWrite(trusted, item, entry, state, next)
			if err != nil {
				return err
			}
			if r.Status == syncmodel.Applied {
				if err := store.SaveState(ctx, tx, r.State); err != nil {
					return err
				}
				if err := s.stateActivity(ctx, tx, a, item, state, r.State, receipt); err != nil {
					return err
				}
			} else {
				result.Except = append(result.Except, id)
			}
			result.Results = append(result.Results, r)
		}
		if _, err := s.change(ctx, tx, a.UserID, syncmodel.StateRange, map[string]any{"range": result}); err != nil {
			return err
		}
		return saveStateOperation(ctx, tx, a.PrincipalID, w.OpID, stateOperation{Kind: "range", Range: &result}, receipt)
	})
	if err != nil {
		return syncmodel.RangeResult{}, syncError(err)
	}
	return result, nil
}

func resolveStateScope(ctx context.Context, tx pgx.Tx, user int64, scope syncmodel.Scope) ([]syncmodel.ID, error) {
	ids := make([]int64, len(scope.IDs))
	for i, id := range scope.IDs {
		if id <= 0 {
			return nil, ErrInvalid
		}
		ids[i] = int64(id)
	}
	query := `SELECT id FROM subscriptions WHERE user_id=$1 AND deleted_at IS NULL`
	args := []any{user}
	switch scope.Kind {
	case syncmodel.ScopeAll:
		if len(ids) != 0 {
			return nil, ErrInvalid
		}
	case syncmodel.ScopeSubscriptions:
		if len(ids) == 0 {
			return nil, ErrInvalid
		}
		query += " AND id=ANY($2)"
		args = append(args, ids)
	case syncmodel.ScopeFolder:
		if len(ids) != 1 {
			return nil, ErrInvalid
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM folders WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)", ids[0], user).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		query += " AND folder_id=$2"
		args = append(args, ids[0])
	default:
		return nil, ErrInvalid
	}
	query += " ORDER BY id"
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve scope: %w", err)
	}
	defer rows.Close()
	out := []syncmodel.ID{}
	for rows.Next() {
		var id syncmodel.ID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// An explicit scope cannot smuggle a different user's subscription ID.
	if scope.Kind == syncmodel.ScopeSubscriptions {
		found := map[syncmodel.ID]bool{}
		for _, id := range out {
			found[id] = true
		}
		for _, id := range scope.IDs {
			if !found[id] {
				return nil, ErrNotFound
			}
		}
	}
	return out, nil
}

func validateStateScope(scope syncmodel.Scope) error {
	for _, id := range scope.IDs {
		if id <= 0 {
			return ErrInvalid
		}
	}
	switch scope.Kind {
	case syncmodel.ScopeAll:
		if len(scope.IDs) != 0 {
			return ErrInvalid
		}
	case syncmodel.ScopeSubscriptions:
		if len(scope.IDs) == 0 {
			return ErrInvalid
		}
	case syncmodel.ScopeFolder:
		if len(scope.IDs) != 1 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Cluster resolves current membership once, then applies ordinary per-entry writes.
func (s *Service) Cluster(ctx context.Context, a Actor, clusterID int64, template syncmodel.Write) ([]syncmodel.Result, error) {
	if err := s.stateActor(ctx, a); err != nil {
		return nil, err
	}
	if clusterID <= 0 || template.OpID == "" {
		return nil, ErrInvalid
	}
	var results []syncmodel.Result
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		current, err := stateRevision(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if err := s.stateTarget(ctx, tx, a, template.Principal); err != nil {
			return err
		}
		validation := template
		validation.EntryID = 1
		trusted := syncmodel.Context{Actor: syncmodel.ID(a.PrincipalID), Token: a.SyncTokenKind(), Receipt: s.now().UTC()}
		if _, err := syncmodel.ResolveWrite(trusted, validation, syncmodel.Entry{}, syncmodel.InitialState(template.Principal, 1), syncmodel.Revision{Generation: current.Generation, Rev: current.Rev + 1}); err != nil {
			return err
		}
		old, err := loadStateOperation(ctx, tx, a.PrincipalID, template.OpID)
		if err != nil {
			return err
		}
		if old != nil {
			if old.Kind != "cluster" {
				return ErrInvalid
			}
			results = make([]syncmodel.Result, len(old.Results))
			for i, result := range old.Results {
				results[i], err = replayState(ctx, tx, a.UserID, result)
				if err != nil {
					return err
				}
			}
			return nil
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM clusters WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL)", a.UserID, clusterID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `SELECT ec.entry_id FROM entry_clusters ec JOIN entries e ON e.id=ec.entry_id JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=ec.user_id WHERE ec.user_id=$1 AND ec.cluster_id=$2 AND ec.deleted_at IS NULL AND e.deleted_at IS NULL AND s.deleted_at IS NULL ORDER BY ec.entry_id`, a.UserID, clusterID)
		if err != nil {
			return err
		}
		writes := []syncmodel.Write{}
		for rows.Next() {
			var id syncmodel.ID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			w := template
			w.EntryID = id
			w.OpID = template.OpID + "/" + strconv.FormatInt(int64(id), 10)
			writes = append(writes, w)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(writes) > maxStateBatch {
			return ErrInvalid
		}
		results, err = s.batchTx(ctx, tx, a, writes, current, s.now().UTC())
		if err != nil {
			return err
		}
		return saveStateOperation(ctx, tx, a.PrincipalID, template.OpID, stateOperation{Kind: "cluster", Results: results}, s.now().UTC())
	})
	return results, syncError(err)
}

// GoogleWrite translates compatibility intent with receipt-time clocks and zero bases.
func (s *Service) GoogleWrite(ctx context.Context, a Actor, action syncmodel.GoogleAction, entryIDs []int64, opID string) ([]syncmodel.Result, error) {
	if err := s.stateActor(ctx, a); err != nil {
		return nil, err
	}
	if len(entryIDs) > maxStateBatch || len(entryIDs) == 0 || opID == "" {
		return nil, ErrInvalid
	}
	if a.Kind != PrincipalHuman || a.Via != CredentialCompat {
		return nil, ErrForbidden
	}
	generation, err := s.store.Generation(ctx)
	if err != nil {
		return nil, err
	}
	receipt := s.now().UTC()
	trusted := syncmodel.Context{Actor: syncmodel.ID(a.PrincipalID), Token: a.SyncTokenKind(), Receipt: receipt}
	writes := make([]syncmodel.Write, len(entryIDs))
	for i, id := range entryIDs {
		idOp := opID
		if len(entryIDs) > 1 {
			idOp += "-" + strconv.Itoa(i)
		}
		writes[i], err = syncmodel.GoogleWrite(generation, idOp, syncmodel.ID(a.PrincipalID), syncmodel.ID(id), action, trusted)
		if err != nil {
			return nil, syncError(err)
		}
	}
	return s.Batch(ctx, a, writes)
}
