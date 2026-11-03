package syncmodel

import (
	"fmt"
	"slices"
	"time"
)

type stateKey struct{ principal, entry ID }
type opKey struct {
	actor ID
	id    string
}
type remembered struct {
	principal, entry ID
	status           Status
	rangeResult      *RangeResult
}

type Model struct {
	generation      string
	rev, seq, floor uint64
	entries         map[ID]Entry
	states          map[stateKey]State
	operations      map[opKey]remembered
	changes         []Change
	objects         map[objectKey]Object
	positions       map[positionKey]Position
	notifications   map[opKey]Notification
	withheld        bool
}

func New(generation string) (*Model, error) {
	if generation == "" {
		return nil, ErrInvalid
	}
	return &Model{generation: generation, entries: map[ID]Entry{}, states: map[stateKey]State{}, operations: map[opKey]remembered{}, objects: map[objectKey]Object{}, positions: map[positionKey]Position{}, notifications: map[opKey]Notification{}}, nil
}

func (m *Model) Cursor() Revision          { return Revision{m.generation, m.rev} }
func (m *Model) HighestSeq() uint64        { return m.seq }
func (m *Model) Entry(id ID) (Entry, bool) { e, ok := m.entries[id]; return e, ok }
func (m *Model) State(principal, entry ID) State {
	if s, ok := m.states[stateKey{principal, entry}]; ok {
		return s
	}
	return InitialState(principal, entry)
}

// Ingest is the commit step, after fetching/parsing. A supplied global Seq must
// advance visible order; zero allocates the next sequence in this model.
func (m *Model) Ingest(e Entry, receipt time.Time) (Entry, error) {
	if e.ID <= 0 || e.SubscriptionID <= 0 || receipt.IsZero() || e.Deleted {
		return Entry{}, ErrInvalid
	}
	if _, exists := m.entries[e.ID]; exists {
		return Entry{}, fmt.Errorf("entry identity reused: %w", ErrInvalid)
	}
	if e.Seq == 0 {
		e.Seq = m.seq + 1
	}
	if e.Seq <= m.seq {
		return Entry{}, fmt.Errorf("ingest sequence must advance: %w", ErrInvalid)
	}
	e.FirstSeenAt = receipt.UTC()
	e.Rev = m.rev + 1
	m.seq = e.Seq
	m.entries[e.ID] = e
	m.append(Change{Type: EntryCreated, Entry: &e})
	return e, nil
}

func (m *Model) UpdateEntry(id ID, published time.Time) error {
	e, ok := m.entries[id]
	if !ok || e.Deleted {
		return ErrInvalid
	}
	e.PublishedAt = published
	e.Rev = m.rev + 1
	m.entries[id] = e
	m.append(Change{Type: EntryUpdated, Entry: &e})
	return nil
}

func (m *Model) DeleteEntry(id ID) error {
	e, ok := m.entries[id]
	if !ok || e.Deleted {
		return ErrInvalid
	}
	e.Deleted = true
	e.Rev = m.rev + 1
	m.entries[id] = e
	m.append(Change{Type: EntryDeleted, Entry: &e})
	return nil
}

func validValue(name RegisterName, value Value) bool {
	switch name {
	case Seen:
		return value == Unseen || value == SeenValue
	case Shelf:
		return value == None || value == Later || value == Done
	case Kept:
		return value == False || value == True
	}
	return false
}

func validateWrite(ctx Context, w Write, current Revision) error {
	if w.Base.Generation != current.Generation {
		return ErrGeneration
	}
	if ctx.Actor <= 0 || ctx.Receipt.IsZero() || w.OpID == "" || w.Principal <= 0 || w.EntryID <= 0 || w.OccurredAt.IsZero() || !validValue(w.Register, w.Value) || w.Base.Rev > current.Rev {
		return ErrInvalid
	}
	if _, err := DeriveSource(ctx.Token, w.Kind); err != nil {
		return err
	}
	return nil
}

func rank(source Source) int {
	switch source {
	case SourceDeliberate:
		return 3
	case SourceRuleOrAgent:
		return 2
	case SourcePassive:
		return 1
	}
	return 0
}

func wins(name RegisterName, current, next Register, base uint64) bool {
	if base == current.Rev {
		return true
	}
	if name == Seen && current.Source == SourcePassive && next.Source == SourcePassive {
		return next.Value == SeenValue
	}
	if rank(next.Source) != rank(current.Source) {
		return rank(next.Source) > rank(current.Source)
	}
	if !next.OccurredAt.Equal(current.OccurredAt) {
		return next.OccurredAt.After(current.OccurredAt)
	}
	return next.Rev > current.Rev
}

// ResolveWrite is the service's row-level rule implementation. revision is the
// allocated change revision in the current transaction. The service must check
// op_id deduplication first and persist the result with its compact change.
// A zero Entry represents an absent/deleted body. No Model or I/O is required.
func ResolveWrite(ctx Context, w Write, e Entry, s State, revision Revision) (Result, error) {
	if revision.Generation == "" || revision.Rev == 0 {
		return Result{}, ErrInvalid
	}
	if err := validateWrite(ctx, w, Revision{Generation: revision.Generation, Rev: revision.Rev - 1}); err != nil {
		return Result{}, err
	}
	if s.Principal != w.Principal || s.EntryID != w.EntryID || (e.ID != 0 && e.ID != w.EntryID) || !validValue(Seen, s.Seen.Value) || !validValue(Shelf, s.Shelf.Value) || !validValue(Kept, s.Kept.Value) || s.StateRev > revision.Rev || s.Seen.Rev > s.StateRev || s.Shelf.Rev > s.StateRev || s.Kept.Rev > s.StateRev {
		return Result{}, ErrInvalid
	}
	rev := revision.Rev
	r := Result{OpID: w.OpID, State: s}
	if e.ID == 0 || e.Deleted {
		r.Status = Gone
		return r, nil
	}
	occurred := OccurredAt(w.OccurredAt, ctx.Receipt)
	if ctx.Receipt.Sub(occurred) > MaxQueueAge {
		r.Status = Expired
		return r, nil
	}
	source, _ := DeriveSource(ctx.Token, w.Kind)
	next := Register{Value: w.Value, Rev: rev, OccurredAt: occurred, Actor: ctx.Actor, Source: source}
	current, _ := s.Register(w.Register)
	// The passive merge is a resolved value, including two passive unseens.
	if w.Register == Seen && w.Base.Rev != current.Rev && current.Source == SourcePassive && next.Source == SourcePassive && current.Value == Unseen {
		next.Value = SeenValue
	}
	base := w.Base.Rev
	// An earlier item in this transaction may have cleared Done. That causal
	// side effect is not a concurrent device write; later batch intent applies.
	if current.Rev == rev {
		base = current.Rev
	}
	if !wins(w.Register, current, next, base) {
		r.Status = Superseded
		return r, nil
	}
	switch w.Register {
	case Seen:
		s.Seen = next
		// Unread is one invariant, including when the shelf write would otherwise
		// outrank it. The side effect shares the accepted action's provenance.
		if next.Value == Unseen && s.Shelf.Value == Done {
			s.Shelf = next
			s.Shelf.Value = None
		}
	case Shelf:
		s.Shelf = next
	case Kept:
		s.Kept = next
	}
	s.StateRev = rev
	r.Status = Applied
	r.State = s
	return r, nil
}

func (m *Model) resolve(ctx Context, w Write, rev uint64) Result {
	r, err := ResolveWrite(ctx, w, m.entries[w.EntryID], m.State(w.Principal, w.EntryID), Revision{Generation: m.generation, Rev: rev})
	if err != nil {
		panic(fmt.Sprintf("validated model state violated resolver invariant: %v", err))
	}
	if r.Status == Applied {
		m.states[stateKey{w.Principal, w.EntryID}] = r.State
	}
	return r
}

func (m *Model) replay(key opKey) (Result, bool) {
	old, ok := m.operations[key]
	if !ok {
		return Result{}, false
	}
	r := Result{OpID: key.id, Status: old.status, State: m.State(old.principal, old.entry), Replay: true}
	if e, exists := m.entries[old.entry]; !exists || e.Deleted {
		r.Status = Gone
	}
	return r, true
}

// Batch validates every input before changing anything. Applied registers share
// one change revision; duplicate register targets in a batch are rejected.
func (m *Model) Batch(ctx Context, writes []Write) ([]Result, error) {
	targets := map[struct {
		key  stateKey
		name RegisterName
	}]bool{}
	ids := map[string]bool{}
	for i, w := range writes {
		if err := validateWrite(ctx, w, m.Cursor()); err != nil {
			return nil, fmt.Errorf("batch item %d: %w", i, err)
		}
		target := struct {
			key  stateKey
			name RegisterName
		}{stateKey{w.Principal, w.EntryID}, w.Register}
		if targets[target] || ids[w.OpID] {
			return nil, fmt.Errorf("duplicate batch target or op_id: %w", ErrInvalid)
		}
		if old, ok := m.operations[opKey{ctx.Actor, w.OpID}]; ok && old.rangeResult != nil {
			return nil, ErrInvalid
		}
		targets[target] = true
		ids[w.OpID] = true
	}
	results := make([]Result, 0, len(writes))
	applied := false
	for _, w := range writes {
		key := opKey{ctx.Actor, w.OpID}
		if r, ok := m.replay(key); ok {
			results = append(results, r)
			continue
		}
		r := m.resolve(ctx, w, m.rev+1)
		results = append(results, r)
		applied = applied || r.Status == Applied
		m.operations[key] = remembered{principal: w.Principal, entry: w.EntryID, status: r.Status}
	}
	// Return the final effective state even when this batch changed two registers.
	for i := range results {
		results[i].State = m.State(results[i].State.Principal, results[i].State.EntryID)
	}
	if applied {
		m.append(Change{Type: StateUpdated, Results: results})
	}
	return results, nil
}

func (m *Model) subscriptions(scope Scope) ([]ID, error) {
	ids := map[ID]bool{}
	switch scope.Kind {
	case ScopeAll:
		for _, e := range m.entries {
			ids[e.SubscriptionID] = true
		}
	case ScopeSubscriptions:
		if len(scope.IDs) == 0 {
			return nil, ErrInvalid
		}
		for _, id := range scope.IDs {
			if id <= 0 {
				return nil, ErrInvalid
			}
			ids[id] = true
		}
	case ScopeFolder:
		if len(scope.IDs) != 1 || scope.IDs[0] <= 0 {
			return nil, ErrInvalid
		}
		for _, o := range m.objects {
			if o.Kind == SubscriptionObject && !o.Deleted && o.FolderID == scope.IDs[0] {
				ids[o.ID] = true
			}
		}
	default:
		return nil, ErrInvalid
	}
	out := make([]ID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

// Through commits a range atomically. Storage may prepare chunks, but none may
// become visible until this result and its single event can commit together.
func (m *Model) Through(ctx Context, request RangeWrite) (RangeResult, error) {
	w := request.Write
	// EntryID is not a range target; use a valid sentinel for common validation.
	w.EntryID = 1
	if err := validateWrite(ctx, w, m.Cursor()); err != nil {
		return RangeResult{}, err
	}
	if w.Register != Seen || w.Value != SeenValue {
		return RangeResult{}, ErrInvalid
	}
	subs, err := m.subscriptions(request.Scope)
	if err != nil {
		return RangeResult{}, err
	}
	key := opKey{ctx.Actor, w.OpID}
	if old, ok := m.operations[key]; ok {
		if old.rangeResult == nil {
			return RangeResult{}, ErrInvalid
		}
		r := cloneRange(*old.rangeResult)
		r.Replay = true
		for i := range r.Results {
			r.Results[i].State = m.State(r.Results[i].State.Principal, r.Results[i].State.EntryID)
			r.Results[i].Replay = true
			if e := m.entries[r.Results[i].State.EntryID]; e.Deleted {
				r.Results[i].Status = Gone
			}
		}
		return r, nil
	}
	r := RangeResult{Status: Applied, ThroughSeq: request.ThroughSeq, Subscriptions: subs, Except: []ID{}, Results: []Result{}}
	if ctx.Receipt.Sub(OccurredAt(w.OccurredAt, ctx.Receipt)) > MaxQueueAge {
		r.Status = Expired
	} else {
		ids := []ID{}
		for id, e := range m.entries {
			if !e.Deleted && e.Seq <= r.ThroughSeq && slices.Contains(subs, e.SubscriptionID) {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			item := w
			item.EntryID = id
			result := m.resolve(ctx, item, m.rev+1)
			r.Results = append(r.Results, result)
			if result.Status != Applied {
				r.Except = append(r.Except, id)
			}
		}
		m.append(Change{Type: StateRange, Range: &r})
	}
	saved := cloneRange(r)
	m.operations[key] = remembered{rangeResult: &saved}
	return r, nil
}

// Cluster resolves current membership in the service and uses Batch, never a
// range that could accidentally include a member ingested later.
func (m *Model) Cluster(ctx Context, writes []Write) ([]Result, error) { return m.Batch(ctx, writes) }

// ThroughAt maps Google Reader ts; nil uses the receipt-visible highest seq.
func (m *Model) ThroughAt(ts *time.Time) uint64 {
	if ts == nil {
		return m.seq
	}
	var through uint64
	for _, e := range m.entries {
		if !e.FirstSeenAt.After(*ts) && e.Seq > through {
			through = e.Seq
		}
	}
	return through
}

func (m *Model) append(change Change) {
	m.rev++
	change.Rev = m.rev
	m.changes = append(m.changes, cloneChange(change))
}

func (m *Model) Changes(cursor Revision, limit int) ([]Change, Revision, error) {
	if cursor.Generation != m.generation {
		return nil, Revision{}, ErrGeneration
	}
	if cursor.Rev < m.floor || cursor.Rev > m.rev {
		return nil, Revision{}, ErrCursorGone
	}
	if limit <= 0 {
		return nil, Revision{}, ErrInvalid
	}
	out := []Change{}
	next := cursor
	for _, c := range m.changes {
		if c.Rev <= cursor.Rev {
			continue
		}
		out = append(out, cloneChange(c))
		next.Rev = c.Rev
		if len(out) == limit {
			break
		}
	}
	return out, next, nil
}

// RetainAfter discards events through rev; a cursor exactly at rev is valid.
func (m *Model) RetainAfter(rev uint64) error {
	if rev < m.floor || rev > m.rev {
		return ErrInvalid
	}
	m.floor = rev
	m.changes = slices.DeleteFunc(m.changes, func(c Change) bool { return c.Rev <= rev })
	return nil
}
