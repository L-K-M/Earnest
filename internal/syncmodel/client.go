package syncmodel

import "slices"

type pendingKey struct {
	principal, entry ID
	register         RegisterName
}

// Pending is a tagged outbox item: exactly one of Write or Range is present.
type Pending struct {
	Write *Write      `json:"write,omitempty"`
	Range *RangeWrite `json:"range,omitempty"`
}

func (p Pending) operation() Write {
	if p.Write != nil {
		return *p.Write
	}
	return p.Range.Write
}

type Client struct {
	cursor    Revision
	entries   map[ID]Entry
	states    map[stateKey]State
	objects   map[objectKey]Object
	positions map[positionKey]Position
	pending   []Pending
}

func NewClient(cursor Revision) *Client {
	return &Client{cursor: cursor, entries: map[ID]Entry{}, states: map[stateKey]State{}, objects: map[objectKey]Object{}, positions: map[positionKey]Position{}}
}
func (c *Client) Cursor() Revision { return c.cursor }

// BeginBootstrap is called with the cursor read BEFORE paging resources. It
// replaces the cache but retains local intent. Old-generation intent is made
// stale rather than rebased onto a revision that would force it to win.
func (c *Client) BeginBootstrap(cursor Revision) {
	if cursor.Generation != c.cursor.Generation {
		for i := range c.pending {
			if c.pending[i].Write != nil {
				c.pending[i].Write.Base = Revision{Generation: cursor.Generation}
			} else {
				c.pending[i].Range.Write.Base = Revision{Generation: cursor.Generation}
			}
		}
	}
	c.cursor = cursor
	c.entries = map[ID]Entry{}
	c.states = map[stateKey]State{}
	c.objects = map[objectKey]Object{}
	c.positions = map[positionKey]Position{}
}

func (c *Client) MergeEntry(e Entry) {
	if old, ok := c.entries[e.ID]; !ok || (!old.Deleted && e.Rev > old.Rev) {
		c.entries[e.ID] = e
	}
}
func mergeState(old, next State) State {
	if next.Seen.Rev > old.Seen.Rev {
		old.Seen = next.Seen
	}
	if next.Shelf.Rev > old.Shelf.Rev {
		old.Shelf = next.Shelf
	}
	if next.Kept.Rev > old.Kept.Rev {
		old.Kept = next.Kept
	}
	if next.StateRev > old.StateRev {
		old.StateRev = next.StateRev
	}
	return old
}
func (c *Client) serverState(principal, entry ID) State {
	if s, ok := c.states[stateKey{principal, entry}]; ok {
		return s
	}
	return InitialState(principal, entry)
}
func (c *Client) MergeState(s State) {
	key := stateKey{s.Principal, s.EntryID}
	c.states[key] = mergeState(c.serverState(s.Principal, s.EntryID), s)
}
func (c *Client) MergeObject(o Object) {
	key := objectKey{o.Kind, o.ID}
	if old, ok := c.objects[key]; !ok || o.Rev > old.Rev {
		c.objects[key] = o
	}
}
func (c *Client) MergePosition(p Position) {
	key := positionKey{p.Principal, p.View}
	if old, ok := c.positions[key]; !ok || p.Rev > old.Rev {
		c.positions[key] = p
	}
}
func (c *Client) Entry(id ID) (Entry, bool) { e, ok := c.entries[id]; return e, ok }
func (c *Client) Object(kind ObjectKind, id ID) (Object, bool) {
	o, ok := c.objects[objectKey{kind, id}]
	return o, ok
}
func (c *Client) Position(principal ID, view string) (Position, bool) {
	p, ok := c.positions[positionKey{principal, view}]
	return p, ok
}

// ApplyPage is atomic, including its cursor. Persist cache and cursor together.
// Replaying the same page is safe; a gap or a partial page is rejected.
func (c *Client) ApplyPage(changes []Change, next Revision) error {
	if next.Generation != c.cursor.Generation {
		return ErrGeneration
	}
	expected := c.cursor.Rev
	for _, change := range changes {
		if change.Rev <= expected {
			continue
		}
		if change.Rev != expected+1 {
			return ErrInvalid
		}
		expected = change.Rev
	}
	if next.Rev != expected {
		return ErrInvalid
	}
	for _, change := range changes {
		if change.Rev <= c.cursor.Rev {
			continue
		}
		if change.Entry != nil {
			c.MergeEntry(*change.Entry)
		}
		if change.Object != nil {
			c.MergeObject(*change.Object)
		}
		if change.Position != nil {
			c.MergePosition(*change.Position)
		}
		results := change.Results
		if change.Range != nil {
			results = change.Range.Results
		}
		for _, r := range results {
			e, ok := c.entries[r.State.EntryID]
			if !ok || e.Deleted {
				continue
			}
			c.MergeState(r.State)
		}
	}
	c.cursor = next
	return nil
}

// Queue coalesces register intent. Clearing Done is retained as a separate
// shelf intent so a subsequent coalesced seen action cannot resurrect Done.
func (c *Client) Queue(w Write) error {
	if w.OpID == "" || w.Principal <= 0 || w.EntryID <= 0 || w.OccurredAt.IsZero() || !validValue(w.Register, w.Value) || w.Base.Generation != c.cursor.Generation || (w.Kind != Explicit && w.Kind != Passive) {
		return ErrInvalid
	}
	clearDone := w.Register == Seen && w.Value == Unseen && c.State(w.Principal, w.EntryID).Shelf.Value == Done
	for _, old := range c.pending {
		id := old.operation().OpID
		if id == w.OpID || (clearDone && id == w.OpID+"/clear-done") {
			return ErrInvalid
		}
	}
	if clearDone {
		shelf := w
		shelf.OpID += "/clear-done"
		shelf.Register = Shelf
		shelf.Value = None
		shelf.Base.Rev = c.serverState(w.Principal, w.EntryID).Shelf.Rev
		c.queueWrite(shelf)
	}
	c.queueWrite(w)
	return nil
}
func (c *Client) queueWrite(w Write) {
	for i, old := range c.pending {
		if old.Write == nil {
			continue
		}
		previous := old.Write
		if (pendingKey{previous.Principal, previous.EntryID, previous.Register}) == (pendingKey{w.Principal, w.EntryID, w.Register}) {
			w.Base = previous.Base
			c.pending = slices.Delete(c.pending, i, i+1)
			break
		}
	}
	c.pending = append(c.pending, Pending{Write: &w})
}

// QueueRange retains receipt-resolved boundaries, original clocks and scope.
// It cannot expand to cached entries: uncached entries also belong to a range.
func (c *Client) QueueRange(r RangeWrite) error {
	w := r.Write
	if w.OpID == "" || w.Principal <= 0 || w.Base.Generation != c.cursor.Generation || w.OccurredAt.IsZero() || w.Register != Seen || w.Value != SeenValue || (w.Kind != Explicit && w.Kind != Passive) {
		return ErrInvalid
	}
	switch r.Scope.Kind {
	case ScopeAll:
		if len(r.Scope.IDs) != 0 {
			return ErrInvalid
		}
	case ScopeSubscriptions:
		if len(r.Scope.IDs) == 0 {
			return ErrInvalid
		}
	case ScopeFolder:
		if len(r.Scope.IDs) != 1 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	for _, id := range r.Scope.IDs {
		if id <= 0 {
			return ErrInvalid
		}
	}
	for _, old := range c.pending {
		if old.operation().OpID == w.OpID {
			return ErrInvalid
		}
	}
	r.Scope.IDs = slices.Clone(r.Scope.IDs)
	c.pending = append(c.pending, Pending{Range: &r})
	return nil
}

func (c *Client) Outbox() []Pending {
	out := make([]Pending, 0, len(c.pending))
	for _, p := range c.pending {
		if p.Write != nil {
			w := *p.Write
			p.Write = &w
		} else {
			r := *p.Range
			r.Scope.IDs = slices.Clone(r.Scope.IDs)
			p.Range = &r
		}
		out = append(out, p)
	}
	return out
}

func (c *Client) rangeContains(r RangeWrite, e Entry) bool {
	if e.Seq > r.ThroughSeq {
		return false
	}
	switch r.Scope.Kind {
	case ScopeAll:
		return true
	case ScopeSubscriptions:
		return slices.Contains(r.Scope.IDs, e.SubscriptionID)
	case ScopeFolder:
		o, ok := c.objects[objectKey{SubscriptionObject, e.SubscriptionID}]
		return ok && !o.Deleted && o.FolderID == r.Scope.IDs[0]
	}
	return false
}

func (c *Client) State(principal, entry ID) State {
	s := c.serverState(principal, entry)
	e, ok := c.entries[entry]
	if !ok || e.Deleted {
		return s
	}
	for _, pending := range c.pending {
		w := pending.operation()
		if pending.Range != nil {
			if w.Principal == principal && c.rangeContains(*pending.Range, e) {
				s.Seen.Value = SeenValue
			}
			continue
		}
		if w.Principal != principal || w.EntryID != entry {
			continue
		}
		switch w.Register {
		case Seen:
			s.Seen.Value = w.Value
			if w.Value == Unseen && s.Shelf.Value == Done {
				s.Shelf.Value = None
			}
		case Shelf:
			s.Shelf.Value = w.Value
		case Kept:
			s.Kept.Value = w.Value
		}
	}
	return s
}

// Acknowledge removes only the exact operation, preserving a newer coalesced
// replacement when an earlier in-flight response arrives.
func (c *Client) Acknowledge(results []Result) {
	for _, r := range results {
		c.MergeState(r.State)
		if r.Status == Gone {
			if e, ok := c.entries[r.State.EntryID]; ok {
				e.Deleted = true
				c.entries[e.ID] = e
			}
		}
		c.pending = slices.DeleteFunc(c.pending, func(p Pending) bool { return p.operation().OpID == r.OpID })
	}
}

// AcknowledgeRange adopts resolved states and removes this exact queued range.
func (c *Client) AcknowledgeRange(opID string, r RangeResult) {
	for _, result := range r.Results {
		c.MergeState(result.State)
	}
	c.pending = slices.DeleteFunc(c.pending, func(p Pending) bool { return p.operation().OpID == opID })
}
