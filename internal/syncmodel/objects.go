package syncmodel

import "time"

type ObjectKind string

const (
	SubscriptionObject ObjectKind = "subscription"
	FolderObject       ObjectKind = "folder"
	LabelObject        ObjectKind = "label"
	AnnotationObject   ObjectKind = "annotation"
	TokenObject        ObjectKind = "token"
)

type Object struct {
	Kind      ObjectKind `json:"kind"`
	ID        ID         `json:"id"`
	Principal ID         `json:"principal"`
	EntryID   ID         `json:"entry_id,omitempty"`
	FolderID  ID         `json:"folder_id,omitempty"`
	Name      string     `json:"name,omitempty"`
	Rev       uint64     `json:"rev"`
	Deleted   bool       `json:"deleted"`
}

type objectKey struct {
	kind ObjectKind
	id   ID
}
type positionKey struct {
	principal ID
	view      string
}

type Position struct {
	Principal ID     `json:"principal"`
	View      string `json:"view"`
	Seq       uint64 `json:"seq"`
	Rev       uint64 `json:"rev"`
}

// PutObject preserves removals as revisioned tombstones. Label IDs identify
// entry-label memberships, not names shared by unrelated entries.
func (m *Model) PutObject(o Object) (Object, error) {
	if o.ID <= 0 {
		return Object{}, ErrInvalid
	}
	old, exists := m.objects[objectKey{o.Kind, o.ID}]
	var kind ChangeType
	switch o.Kind {
	case SubscriptionObject:
		kind = SubscriptionCreated
		if exists {
			kind = SubscriptionUpdated
		}
		if o.Deleted {
			kind = SubscriptionDeleted
		}
	case FolderObject:
		kind = FolderUpdated
		if o.Deleted {
			kind = FolderDeleted
		}
	case LabelObject:
		if o.EntryID <= 0 || o.Name == "" || o.Principal <= 0 {
			return Object{}, ErrInvalid
		}
		e, ok := m.entries[o.EntryID]
		if !ok || e.Deleted {
			return Object{}, ErrInvalid
		}
		if exists && (old.EntryID != o.EntryID || old.Principal != o.Principal || old.Name != o.Name) {
			return Object{}, ErrInvalid
		}
		kind = LabelAdded
		if o.Deleted {
			kind = LabelRemoved
		}
	case AnnotationObject:
		kind = AnnotationUpdated
		if o.Deleted {
			kind = AnnotationDeleted
		}
	case TokenObject:
		if !o.Deleted {
			return Object{}, ErrInvalid
		}
		kind = TokenRevoked
	default:
		return Object{}, ErrInvalid
	}
	o.Rev = m.rev + 1
	m.objects[objectKey{o.Kind, o.ID}] = o
	m.append(Change{Type: kind, Object: &o})
	return o, nil
}

func (m *Model) Object(kind ObjectKind, id ID) (Object, bool) {
	o, ok := m.objects[objectKey{kind, id}]
	return o, ok
}

// PutPosition uses receipt/commit order, not client clocks, and never marks seen.
func (m *Model) PutPosition(p Position) (Position, error) {
	if p.Principal <= 0 || p.View == "" || p.Seq > m.seq {
		return Position{}, ErrInvalid
	}
	p.Rev = m.rev + 1
	m.positions[positionKey{p.Principal, p.View}] = p
	m.append(Change{Type: PositionUpdated, Position: &p})
	return p, nil
}
func (m *Model) Position(principal ID, view string) (Position, bool) {
	p, ok := m.positions[positionKey{principal, view}]
	return p, ok
}

type NotificationStatus string

const (
	NotificationPending   NotificationStatus = "pending"
	NotificationAttempted NotificationStatus = "uncertain"
	NotificationDelivered NotificationStatus = "delivered"
)

type Notification struct {
	Actor       ID                 `json:"actor"`
	OpID        string             `json:"op_id"`
	Generation  string             `json:"generation"`
	Status      NotificationStatus `json:"status"`
	Replay      bool               `json:"replay"`
	AttemptedAt time.Time          `json:"attempted_at"`
}

// AcceptNotification checks generation BEFORE deduplication, including when a
// restored snapshot predates both the operation and its idempotency record.
func (m *Model) AcceptNotification(actor ID, opID, generation string) (Notification, error) {
	if generation != m.generation {
		return Notification{}, ErrGeneration
	}
	if actor <= 0 || opID == "" {
		return Notification{}, ErrInvalid
	}
	key := opKey{actor, opID}
	if n, ok := m.notifications[key]; ok {
		n.Replay = true
		return n, nil
	}
	n := Notification{Actor: actor, OpID: opID, Generation: generation, Status: NotificationPending}
	m.notifications[key] = n
	return n, nil
}

// AttemptNotification must be persisted before the transport is called. A
// crash after this transition loses delivery rather than risking a duplicate.
func (m *Model) AttemptNotification(actor ID, opID string, receipt time.Time) (Notification, error) {
	if receipt.IsZero() {
		return Notification{}, ErrInvalid
	}
	n, ok := m.notifications[opKey{actor, opID}]
	if !ok {
		return Notification{}, ErrInvalid
	}
	if n.Status != NotificationPending {
		return n, ErrAttempted
	}
	if m.withheld || n.Generation != m.generation {
		return n, ErrWithheld
	}
	n.Status = NotificationAttempted
	n.AttemptedAt = receipt.UTC()
	m.notifications[opKey{actor, opID}] = n
	return n, nil
}

func (m *Model) CompleteNotification(actor ID, opID string) error {
	key := opKey{actor, opID}
	n, ok := m.notifications[key]
	if !ok || n.Status != NotificationAttempted {
		return ErrInvalid
	}
	n.Status = NotificationDelivered
	m.notifications[key] = n
	return nil
}
func (m *Model) Notification(actor ID, opID string) (Notification, bool) {
	n, ok := m.notifications[opKey{actor, opID}]
	return n, ok
}

func (m *Model) ReleaseNotifications() { m.withheld = false }

// Restore requires a unique generation supplied by the durable restore process.
// Notification requests must retain their original generation across retries.
func Restore(snapshot *Model, generation string) (*Model, error) {
	if snapshot == nil || generation == "" || generation == snapshot.generation {
		return nil, ErrInvalid
	}
	m := snapshot.Clone()
	m.generation = generation
	m.withheld = true
	return m, nil
}

func (m *Model) Clone() *Model {
	out, _ := New(m.generation)
	out.rev = m.rev
	out.seq = m.seq
	out.floor = m.floor
	out.withheld = m.withheld
	for k, v := range m.entries {
		out.entries[k] = v
	}
	for k, v := range m.states {
		out.states[k] = v
	}
	for k, v := range m.objects {
		out.objects[k] = v
	}
	for k, v := range m.positions {
		out.positions[k] = v
	}
	for k, v := range m.notifications {
		out.notifications[k] = v
	}
	for k, v := range m.operations {
		if v.rangeResult != nil {
			r := cloneRange(*v.rangeResult)
			v.rangeResult = &r
		}
		out.operations[k] = v
	}
	for _, c := range m.changes {
		out.changes = append(out.changes, cloneChange(c))
	}
	return out
}
func cloneRange(r RangeResult) RangeResult {
	r.Subscriptions = append([]ID{}, r.Subscriptions...)
	r.Except = append([]ID{}, r.Except...)
	r.Results = append([]Result{}, r.Results...)
	return r
}
func cloneChange(c Change) Change {
	c.Results = append([]Result(nil), c.Results...)
	if c.Entry != nil {
		e := *c.Entry
		c.Entry = &e
	}
	if c.Range != nil {
		r := cloneRange(*c.Range)
		c.Range = &r
	}
	if c.Object != nil {
		o := *c.Object
		c.Object = &o
	}
	if c.Position != nil {
		p := *c.Position
		c.Position = &p
	}
	return c
}
