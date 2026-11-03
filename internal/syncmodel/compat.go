package syncmodel

type GoogleAction string

const (
	GoogleRead       GoogleAction = "read"
	GoogleUnread     GoogleAction = "unread"
	GoogleKeptUnread GoogleAction = "kept-unread"
	GoogleStar       GoogleAction = "star"
	GoogleUnstar     GoogleAction = "unstar"
)

// GoogleWrite deliberately uses a zero base: this protocol cannot distinguish
// an offline replay from a fresh action. Unread clears Done in the shared rule.
// Labels use PutObject; mark-all uses ThroughAt followed by Through.
func GoogleWrite(generation, opID string, principal, entry ID, action GoogleAction, ctx Context) (Write, error) {
	w := Write{OpID: opID, Principal: principal, EntryID: entry, Base: Revision{Generation: generation}, OccurredAt: ctx.Receipt, Kind: Explicit}
	switch action {
	case GoogleRead:
		w.Register = Seen
		w.Value = SeenValue
		w.Kind = Passive
	case GoogleUnread, GoogleKeptUnread:
		w.Register = Seen
		w.Value = Unseen
	case GoogleStar:
		w.Register = Kept
		w.Value = True
	case GoogleUnstar:
		w.Register = Kept
		w.Value = False
	default:
		return Write{}, ErrInvalid
	}
	return w, nil
}
