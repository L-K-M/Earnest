package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// These steps deliberately use the same files as the pure model. Ingest, writes,
// ranges, compatibility, positions and pulls cross the real service boundary.
// The notification and mixed annotation/token fixture belong to later packages.
var coreVectorFiles = []string{
	"01-range-exception.json", "02-queue-age.json", "03-retention-deleted.json", "04-source-ranks.json", "05-equal-time.json", "06-backdated-ingest.json", "07-publisher-clock.json", "08-expired-cursor.json", "09-restore-generation.json", "10-notification-response-lost.json", "11-shared-ingest.json", "12-offline-google-batch.json", "13-bootstrap-range-race.json", "14-ingest-commit-order.json", "15-repeated-operation.json", "16-google-unread-done.json", "17-independent-kept.json", "18-late-cluster-member.json", "19-notification-before-snapshot.json", "20-google-tradeoff.json", "21-bootstrap-newer-range-crash.json", "22-native-unread-done.json", "23-batch-atomicity.json", "24-label-tombstones.json", "25-position-live.json", "26-missing-ts.json", "27-client-coalescing.json", "28-clock-boundaries.json", "29-resource-tombstones.json", "30-principal-isolation.json", "31-queued-range-client.json", "32-coalesced-done-clear.json", "33-restore-release-safety.json", "34-integer-identity.json", "35-batch-causal-shelf.json",
	"36-ack-out-of-order-merge.json", "37-ack-stale-generation.json", "38-independent-register-merge.json", "39-range-ack-gone.json",
}

type coreVector struct {
	Version    int        `json:"version"`
	Name       string     `json:"name"`
	Decision   string     `json:"decision"`
	Setup      []coreStep `json:"setup"`
	Operations []coreStep `json:"operations"`
	Final      []coreStep `json:"final"`
}
type coreStep struct {
	Op         string                 `json:"op"`
	Server     string                 `json:"server,omitempty"`
	Device     string                 `json:"device,omitempty"`
	Name       string                 `json:"name,omitempty"`
	At         time.Time              `json:"at,omitempty"`
	Actor      syncmodel.ID           `json:"actor,omitempty"`
	Token      syncmodel.TokenKind    `json:"token,omitempty"`
	Entry      syncmodel.Entry        `json:"entry,omitempty"`
	State      syncmodel.State        `json:"state,omitempty"`
	Write      syncmodel.Write        `json:"write,omitempty"`
	Writes     []syncmodel.Write      `json:"writes,omitempty"`
	Scope      syncmodel.Scope        `json:"scope,omitempty"`
	Through    *uint64                `json:"through_seq,omitempty"`
	Timestamp  *time.Time             `json:"ts,omitempty"`
	Action     syncmodel.GoogleAction `json:"action,omitempty"`
	Generation string                 `json:"generation,omitempty"`
	Cursor     syncmodel.Revision     `json:"cursor,omitempty"`
	Rev        uint64                 `json:"rev,omitempty"`
	Object     syncmodel.Object       `json:"object,omitempty"`
	Position   syncmodel.Position     `json:"position,omitempty"`
	Principal  syncmodel.ID           `json:"principal,omitempty"`
	EntryID    syncmodel.ID           `json:"entry_id,omitempty"`
	OpID       string                 `json:"op_id,omitempty"`
	Count      *int                   `json:"count,omitempty"`
	Repeat     int                    `json:"repeat,omitempty"`
	Expect     json.RawMessage        `json:"expect"`
	Error      string                 `json:"error,omitempty"`
}
type coreCaptured struct {
	changes []syncmodel.Change
	next    syncmodel.Revision
}
type coreResponse struct {
	generation  string
	results     []syncmodel.Result
	rangeResult *syncmodel.RangeResult
	opID        string
}
type coreVectorRunner struct {
	t           *testing.T
	f           *coreFixture
	actors      map[string]Actor
	clients     map[string]*syncmodel.Client
	prepared    map[string]syncmodel.Entry
	pages       map[string]coreCaptured
	responses   map[string]coreResponse
	generations map[string]string
	generation  string
	objects     map[string]syncmodel.Object
	snapshots   map[string]map[string][]json.RawMessage
	serial      int
}

func TestServiceVectors(t *testing.T) {
	template := storetest.NewTemplate(t)
	paths, err := filepath.Glob("../../testdata/vectors/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(coreVectorFiles) {
		t.Fatal("register every vector file")
	}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			if !slices.Contains(coreVectorFiles, name) {
				t.Fatal("unknown vector")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var v coreVector
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&v); err != nil {
				t.Fatal(err)
			}
			if decoder.Decode(new(any)) != io.EOF || v.Version != 1 || len(v.Final) == 0 {
				t.Fatal("invalid vector")
			}
			if slices.Contains([]string{"10-notification-response-lost.json", "19-notification-before-snapshot.json", "29-resource-tombstones.json", "33-restore-release-safety.json"}, name) {
				t.Skip("notification/annotation/token lifecycle is outside the assigned service core")
			}
			f := newCoreFixture(t, template)
			generation, err := f.db.Generation(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			r := &coreVectorRunner{t: t, f: f, actors: map[string]Actor{"main": f.a}, clients: map[string]*syncmodel.Client{}, prepared: map[string]syncmodel.Entry{}, pages: map[string]coreCaptured{}, responses: map[string]coreResponse{}, generations: map[string]string{"g1": generation}, generation: "g1", objects: map[string]syncmodel.Object{}, snapshots: map[string]map[string][]json.RawMessage{}}
			steps := append(append(v.Setup, v.Operations...), v.Final...)
			// Shared-feed fixtures have both subscriptions before the first ingest.
			for _, step := range steps {
				if step.Server != "" && step.Server != "main" {
					r.serverActor(step.Server)
				}
			}
			for i, step := range steps {
				got, err := r.execute(step)
				if coreVectorError(err) != step.Error {
					t.Fatalf("step %d %s: error %q want %q: %v", i, step.Op, coreVectorError(err), step.Error, err)
				}
				if len(step.Expect) == 0 {
					t.Fatalf("step %d missing expectation", i)
				}
				actual, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				// Generation aliases are fixture names; production cursors remain UUIDs.
				for alias, uuid := range r.generations {
					actual = bytes.ReplaceAll(actual, []byte(uuid), []byte(alias))
				}
				var observed, expected any
				d := json.NewDecoder(bytes.NewReader(actual))
				d.UseNumber()
				if err := d.Decode(&observed); err != nil {
					t.Fatal(err)
				}
				d = json.NewDecoder(bytes.NewReader(step.Expect))
				d.UseNumber()
				if err := d.Decode(&expected); err != nil {
					t.Fatal(err)
				}
				if err := coreSubset(expected, observed, ""); err != nil {
					t.Fatalf("step %d %s: %v\ngot %s", i, step.Op, err, actual)
				}
			}
		})
	}
}
func coreSubset(want, got any, path string) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s expected object", path)
		}
		for key, value := range w {
			actual, exists := g[key]
			if !exists {
				return fmt.Errorf("%s.%s missing", path, key)
			}
			if err := coreSubset(value, actual, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s array size differs: want %v got %v", path, w, got)
		}
		for i, value := range w {
			if err := coreSubset(value, g[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("%s want %v got %v", path, want, got)
		}
	}
	return nil
}
func coreVectorError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, syncmodel.ErrGeneration):
		return "generation"
	case errors.Is(err, ErrGone), errors.Is(err, syncmodel.ErrCursorGone):
		return "cursor_gone"
	case errors.Is(err, ErrInvalid), errors.Is(err, syncmodel.ErrInvalid):
		return "invalid"
	}
	return err.Error()
}
func (r *coreVectorRunner) serverActor(server string) Actor {
	if server == "" {
		server = "main"
	}
	if a, ok := r.actors[server]; ok {
		return a
	}
	a := r.f.a
	ctx := context.Background()
	if err := r.f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES($1,'$argon2id$test') RETURNING id", server).Scan(&a.UserID); err != nil {
		r.t.Fatal(err)
	}
	if err := r.f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Human') RETURNING id", a.UserID).Scan(&a.PrincipalID); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.f.db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", a.UserID, r.f.feed); err != nil {
		r.t.Fatal(err)
	}
	r.actors[server] = a
	return a
}
func (r *coreVectorRunner) actor(step coreStep) Actor {
	a := r.serverActor(step.Server)
	actor := step.Actor
	if actor == 0 || actor == 1 {
		actor = syncmodel.ID(a.PrincipalID)
	}
	a.PrincipalID = int64(actor)
	switch step.Token {
	case syncmodel.TokenAgent:
		a.Kind = PrincipalAgent
		a.Via = CredentialAgentToken
		a.Scopes = AgentScopes
	case syncmodel.TokenRule:
		a.Kind = PrincipalPipeline
		a.Via = CredentialInternal
	case syncmodel.TokenCompat:
		a.Via = CredentialCompat
	}
	if _, err := r.f.db.Pool().Exec(context.Background(), "INSERT INTO principals(id,user_id,kind,name) VALUES($1,$2,$3,'Fixture') ON CONFLICT(id) DO NOTHING", a.PrincipalID, a.UserID, string(a.Kind)); err != nil {
		r.t.Fatal(err)
	}
	return a
}
func (r *coreVectorRunner) cursor(a Actor) syncmodel.Revision {
	cursor, err := r.f.s.BootstrapCursor(context.Background(), a)
	if err != nil {
		r.t.Fatal(err)
	}
	return cursor
}
func (r *coreVectorRunner) entry(a Actor, id syncmodel.ID) (syncmodel.Entry, bool) {
	var e syncmodel.Entry
	err := r.f.db.Pool().QueryRow(context.Background(), `SELECT e.id,s.id,e.seq,e.first_seen_at,coalesce(e.published_at,'0001-01-01'::timestamptz),coalesce(er.rev,0),e.deleted_at IS NOT NULL FROM entries e JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 LEFT JOIN entry_revisions er ON er.entry_id=e.id AND er.user_id=s.user_id WHERE e.id=$2 ORDER BY s.id DESC LIMIT 1`, a.UserID, int64(id)).Scan(&e.ID, &e.SubscriptionID, &e.Seq, &e.FirstSeenAt, &e.PublishedAt, &e.Rev, &e.Deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, false
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return e, true
}
func (r *coreVectorRunner) ingest(a Actor, e syncmodel.Entry, at time.Time) (syncmodel.Entry, error) {
	if old, ok := r.entry(a, e.ID); ok {
		return old, nil
	}
	ctx := context.Background()
	// Fixture identity is independent of seq. Force only bigserial test identity.
	if _, err := r.f.db.Pool().Exec(ctx, "SELECT setval('entries_id_seq',$1,false)", int64(e.ID)); err != nil {
		return syncmodel.Entry{}, err
	}
	if e.SubscriptionID != 0 {
		var subID int64
		if err := r.f.db.Pool().QueryRow(ctx, "SELECT id FROM subscriptions WHERE user_id=$1 AND feed_id=$2 AND deleted_at IS NULL", a.UserID, r.f.feed).Scan(&subID); err != nil {
			return syncmodel.Entry{}, err
		}
		if subID != int64(e.SubscriptionID) {
			if _, err := r.f.db.Pool().Exec(ctx, "UPDATE subscriptions SET id=$2 WHERE id=$1", subID, int64(e.SubscriptionID)); err != nil {
				return syncmodel.Entry{}, err
			}
		}
	}
	item := coreItem(strconv.FormatInt(int64(e.ID), 10))
	if !e.PublishedAt.IsZero() {
		item.PublishedAt = &e.PublishedAt
	}
	if _, err := r.f.s.RecordFetch(ctx, FetchOutcome{FeedID: r.f.feed, Status: FetchOK, AttemptedAt: at, Items: []IngestItem{item}}); err != nil {
		return syncmodel.Entry{}, err
	}
	stored, _ := r.entry(a, e.ID)
	return stored, nil
}

func (r *coreVectorRunner) changes(a Actor, cursor syncmodel.Revision) ([]syncmodel.Change, syncmodel.Revision, error) {
	out := []syncmodel.Change{}
	for {
		page, err := r.f.s.Changes(context.Background(), a, cursor, 1000)
		if err != nil {
			return out, syncmodel.Revision{}, err
		}
		for _, record := range page.Changes {
			c := syncmodel.Change{Rev: record.Rev, Type: record.Type}
			if err := json.Unmarshal(record.Payload, &c); err != nil {
				return nil, syncmodel.Revision{}, err
			}
			c.Rev = record.Rev
			c.Type = record.Type
			out = append(out, c)
		}
		cursor = page.Next
		if len(page.Changes) < 1000 {
			return out, cursor, nil
		}
	}
}

func (r *coreVectorRunner) execute(step coreStep) (any, error) {
	switch step.Op {
	case "ingest", "prepare_ingest", "commit_ingest", "repeat_ingest", "write", "batch", "cluster", "range", "google", "google_batch", "delete", "update_entry", "retain", "changes", "capture_changes", "bootstrap_begin", "bootstrap_page", "client_pull", "client_apply", "client_queue", "client_queue_range", "client_merge_state", "client_submit", "client_ack", "client_flush", "position", "object", "checkpoint", "restore", "assert_state", "assert_entry", "assert_position", "assert_object", "assert_client", "assert_log":
	default:
		return nil, fmt.Errorf("unknown vector operation %q: %w", step.Op, ErrInvalid)
	}
	ctx := context.Background()
	if step.At.IsZero() {
		step.At = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	}
	r.f.clock.Store(step.At.UnixNano())
	a := r.actor(step)
	principal := step.Principal
	if principal == 0 {
		principal = syncmodel.ID(r.serverActor(step.Server).PrincipalID)
	}
	defaults := func(w syncmodel.Write) syncmodel.Write {
		if w.Principal == 0 {
			w.Principal = principal
		}
		if w.EntryID == 0 {
			w.EntryID = step.EntryID
		}
		if w.Base.Generation == "" {
			w.Base.Generation = r.generations[r.generation]
		} else if uuid, ok := r.generations[w.Base.Generation]; ok {
			w.Base.Generation = uuid
		}
		if w.OccurredAt.IsZero() {
			w.OccurredAt = step.At
		}
		if w.Kind == "" {
			w.Kind = syncmodel.Explicit
		}
		return w
	}
	client := r.clients[step.Device]
	switch step.Op {
	case "ingest":
		return r.ingest(a, step.Entry, step.At)
	case "prepare_ingest":
		r.prepared[step.Name] = step.Entry
		var seq uint64
		if err := r.f.db.Pool().QueryRow(ctx, "SELECT value FROM seq_counter WHERE singleton").Scan(&seq); err != nil {
			return nil, err
		}
		return map[string]any{"seq": seq, "rev": r.cursor(a).Rev}, nil
	case "commit_ingest":
		e, ok := r.prepared[step.Name]
		if !ok {
			return nil, ErrInvalid
		}
		delete(r.prepared, step.Name)
		return r.ingest(a, e, step.At)
	case "repeat_ingest":
		var e syncmodel.Entry
		for i := range step.Repeat {
			input := step.Entry
			input.ID += syncmodel.ID(i)
			var err error
			e, err = r.ingest(a, input, step.At)
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"rev": r.cursor(a).Rev, "seq": e.Seq}, nil
	case "write":
		results, err := r.f.s.Batch(ctx, a, []syncmodel.Write{defaults(step.Write)})
		if err != nil {
			return nil, err
		}
		return results[0], nil
	case "batch", "cluster":
		writes := append([]syncmodel.Write{}, step.Writes...)
		for i := range writes {
			writes[i] = defaults(writes[i])
		}
		return r.f.s.Batch(ctx, a, writes)
	case "range":
		w := defaults(step.Write)
		if w.Register == "" {
			w.Register = syncmodel.Seen
		}
		if w.Value == "" {
			w.Value = syncmodel.SeenValue
		}
		return r.f.s.Through(ctx, a, ThroughRequest{Write: w, Scope: step.Scope, ThroughSeq: step.Through, At: step.Timestamp})
	case "google", "google_batch":
		a.Via = CredentialCompat
		ids := []int64{int64(step.EntryID)}
		if step.Op == "google_batch" {
			ids = make([]int64, step.Repeat)
			for i := range ids {
				ids[i] = int64(step.EntryID) + int64(i)
			}
		}
		results, err := r.f.s.GoogleWrite(ctx, a, step.Action, ids, step.OpID)
		if err != nil {
			return nil, err
		}
		if step.Op == "google" {
			return results[0], nil
		}
		counts := map[syncmodel.Status]int{syncmodel.Applied: 0, syncmodel.Superseded: 0, syncmodel.Gone: 0, syncmodel.Expired: 0}
		for _, result := range results {
			counts[result.Status]++
		}
		return counts, nil
	case "delete":
		// Pin all other entries inside retention and expire only this fixture target.
		_, err := r.f.db.Pool().Exec(ctx, "UPDATE entries SET first_seen_at=$2 WHERE id=$1", int64(step.EntryID), step.At.Add(-100*24*time.Hour))
		if err == nil {
			_, err = r.f.s.RetainEntries(ctx, step.At, 90*24*time.Hour)
		}
		return map[string]any{}, err
	case "update_entry":
		item := coreItem(strconv.FormatInt(int64(step.EntryID), 10))
		item.PublishedAt = &step.Entry.PublishedAt
		_, err := r.f.s.RecordFetch(ctx, FetchOutcome{FeedID: r.f.feed, Status: FetchOK, Items: []IngestItem{item}})
		return map[string]any{}, err
	case "retain":
		// Retention is clock-driven in production; fixture rev pins the old prefix.
		_, err := r.f.db.Pool().Exec(ctx, "UPDATE changes SET created_at=CASE WHEN rev<=$2 THEN $3::timestamptz-interval '100 days' ELSE $3 END WHERE user_id=$1", a.UserID, int64(step.Rev), step.At)
		if err == nil {
			_, err = r.f.s.RetainChanges(ctx, step.At, 90*24*time.Hour)
		}
		return map[string]any{}, err
	case "changes", "capture_changes":
		cursor := step.Cursor
		if cursor.Generation == "" {
			cursor.Generation = r.generations[r.generation]
		} else if uuid, ok := r.generations[cursor.Generation]; ok {
			cursor.Generation = uuid
		}
		changes, next, err := r.changes(a, cursor)
		if step.Op == "capture_changes" && err == nil {
			r.pages[step.Name] = coreCaptured{changes, next}
		}
		types := []syncmodel.ChangeType{}
		revs := []uint64{}
		for _, c := range changes {
			types = append(types, c.Type)
			revs = append(revs, c.Rev)
		}
		return map[string]any{"changes": changes, "types": types, "revs": revs, "next": next}, err
	case "bootstrap_begin":
		cursor := r.cursor(a)
		if client == nil {
			client = syncmodel.NewClient(cursor)
			r.clients[step.Device] = client
		}
		client.BeginBootstrap(cursor)
		return cursor, nil
	case "bootstrap_page":
		if client == nil {
			return nil, ErrInvalid
		}
		// Entries and states are read from the real paged bootstrap, independently.
		for _, kind := range []BootstrapKind{BootstrapEntries, BootstrapState} {
			request := BootstrapRequest{Kind: kind, Limit: 200, Since: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
			for {
				page, err := r.f.s.Bootstrap(ctx, a, request)
				if err != nil {
					return nil, err
				}
				for _, body := range page.Items {
					if kind == BootstrapEntries {
						var e syncmodel.Entry
						if err := json.Unmarshal(body, &e); err != nil {
							return nil, err
						}
						client.MergeEntry(e)
					} else {
						var state syncmodel.State
						if err := json.Unmarshal(body, &state); err != nil {
							return nil, err
						}
						client.MergeState(state)
					}
				}
				if !page.HasMore {
					break
				}
				request.AfterID = page.NextID
				request.AfterPrincipal = page.NextPrincipal
			}
		}
		for _, o := range r.objects {
			client.MergeObject(o)
		}
		positions, err := r.f.s.Positions(ctx, a)
		if err != nil {
			return nil, err
		}
		for _, position := range positions {
			client.MergePosition(position)
		}
		return map[string]any{}, nil
	case "client_pull":
		if client == nil {
			return nil, ErrInvalid
		}
		changes, next, err := r.changes(a, client.Cursor())
		if err != nil {
			return nil, err
		}
		err = client.ApplyPage(changes, next)
		return client.Cursor(), err
	case "client_apply":
		if client == nil {
			return nil, ErrInvalid
		}
		page, ok := r.pages[step.Name]
		if !ok {
			return nil, ErrInvalid
		}
		changes := page.changes
		if step.Count != nil {
			if *step.Count < 0 || *step.Count > len(changes) {
				return nil, ErrInvalid
			}
			changes = changes[:*step.Count]
		}
		err := client.ApplyPage(changes, page.next)
		return client.Cursor(), err
	case "client_queue":
		if client == nil {
			return nil, ErrInvalid
		}
		return map[string]any{}, client.Queue(defaults(step.Write))
	case "client_queue_range":
		if client == nil {
			return nil, ErrInvalid
		}
		w := defaults(step.Write)
		if w.Register == "" {
			w.Register = syncmodel.Seen
		}
		if w.Value == "" {
			w.Value = syncmodel.SeenValue
		}
		var through uint64
		if step.Through != nil {
			through = *step.Through
		} else {
			var err error
			if step.Timestamp == nil {
				err = r.f.db.Pool().QueryRow(ctx, "SELECT value FROM seq_counter WHERE singleton").Scan(&through)
			} else {
				err = r.f.db.Pool().QueryRow(ctx, "SELECT coalesce(max(seq),0) FROM entries WHERE first_seen_at<=$1", *step.Timestamp).Scan(&through)
			}
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{}, client.QueueRange(syncmodel.RangeWrite{Write: w, Scope: step.Scope, ThroughSeq: through})
	case "client_merge_state":
		if client == nil {
			return nil, ErrInvalid
		}
		client.MergeState(step.State)
		return map[string]any{}, nil
	case "client_submit":
		if client == nil || len(client.Outbox()) == 0 {
			return nil, ErrInvalid
		}
		pending := client.Outbox()[0]
		if pending.Range != nil {
			request := *pending.Range
			result, err := r.f.s.Through(ctx, a, ThroughRequest{Write: request.Write, Scope: request.Scope, ThroughSeq: &request.ThroughSeq})
			if err != nil {
				return nil, err
			}
			r.responses[step.Name] = coreResponse{generation: request.Write.Base.Generation, rangeResult: &result, opID: request.Write.OpID}
			return result, nil
		}
		results, err := r.f.s.Batch(ctx, a, []syncmodel.Write{*pending.Write})
		if err != nil {
			return nil, err
		}
		r.responses[step.Name] = coreResponse{generation: pending.Write.Base.Generation, results: results}
		return results[0], nil
	case "client_ack":
		if client == nil {
			return nil, ErrInvalid
		}
		saved, ok := r.responses[step.Name]
		if !ok {
			return nil, ErrInvalid
		}
		if saved.rangeResult != nil {
			return map[string]any{}, client.AcknowledgeRange(saved.opID, saved.generation, *saved.rangeResult)
		}
		return map[string]any{}, client.Acknowledge(saved.generation, saved.results)
	case "client_flush":
		if client == nil {
			return nil, ErrInvalid
		}
		pending := client.Outbox()
		out := []any{}
		for len(pending) > 0 {
			if pending[0].Range != nil {
				request := *pending[0].Range
				result, err := r.f.s.Through(ctx, a, ThroughRequest{Write: request.Write, Scope: request.Scope, ThroughSeq: &request.ThroughSeq})
				if err != nil {
					return nil, err
				}
				if err := client.AcknowledgeRange(request.Write.OpID, request.Write.Base.Generation, result); err != nil {
					return nil, err
				}
				out = append(out, result)
				pending = pending[1:]
				continue
			}
			writes := []syncmodel.Write{}
			for len(pending) > 0 && pending[0].Write != nil {
				writes = append(writes, *pending[0].Write)
				pending = pending[1:]
			}
			results, err := r.f.s.Batch(ctx, a, writes)
			if err != nil {
				return nil, err
			}
			if err := client.Acknowledge(writes[0].Base.Generation, results); err != nil {
				return nil, err
			}
			for _, result := range results {
				out = append(out, result)
			}
		}
		return out, nil
	case "position":
		return r.f.s.PutPosition(ctx, a, step.Position.View, step.Position.Seq, r.nextOp())
	case "object":
		return r.putObject(a, step.Object)
	case "checkpoint":
		r.snapshots[step.Name] = r.snapshot()
		return map[string]any{"rev": r.cursor(a).Rev}, nil
	case "restore":
		if step.Generation == r.generation || r.snapshots[step.Name] == nil {
			return map[string]any{}, syncmodel.ErrGeneration
		}
		if err := r.restore(r.snapshots[step.Name]); err != nil {
			return nil, err
		}
		uuid, err := r.f.db.RotateGeneration(ctx)
		if err != nil {
			return nil, err
		}
		r.generation = step.Generation
		r.generations[step.Generation] = uuid
		return map[string]any{}, nil
	case "assert_state":
		state, err := store.LoadState(ctx, r.f.db.Pool(), int64(principal), int64(step.EntryID))
		if err != nil {
			return nil, err
		}
		if step.Device != "" {
			if client == nil {
				return nil, ErrInvalid
			}
			state = client.State(principal, step.EntryID)
		}
		return map[string]any{"state": state, "unread": state.Unread(), "google_read": state.GoogleRead()}, nil
	case "assert_entry":
		e, exists := r.entry(a, step.EntryID)
		if step.Device != "" {
			if client == nil {
				return nil, ErrInvalid
			}
			e, exists = client.Entry(step.EntryID)
		}
		return map[string]any{"entry": e, "exists": exists, "age_seconds": e.Age(step.At).Seconds()}, nil
	case "assert_position":
		p, err := r.f.s.Position(ctx, a, step.Position.View)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if step.Device != "" {
			if client == nil {
				return nil, ErrInvalid
			}
			p, exists = client.Position(principal, step.Position.View)
		}
		return map[string]any{"position": p, "exists": exists}, nil
	case "assert_object":
		o, exists := r.objects[string(step.Object.Kind)+"/"+strconv.FormatInt(int64(step.Object.ID), 10)]
		if step.Device != "" {
			if client == nil {
				return nil, ErrInvalid
			}
			o, exists = client.Object(step.Object.Kind, step.Object.ID)
		}
		return map[string]any{"object": o, "exists": exists}, nil
	case "assert_client":
		if client == nil {
			return nil, ErrInvalid
		}
		return map[string]any{"outbox_size": len(client.Outbox()), "cursor": client.Cursor()}, nil
	case "assert_log":
		var seq, floor uint64
		if err := r.f.db.Pool().QueryRow(ctx, "SELECT value FROM seq_counter WHERE singleton").Scan(&seq); err != nil {
			return nil, err
		}
		if err := r.f.db.Pool().QueryRow(ctx, "SELECT coalesce((SELECT retention_floor FROM rev_counters WHERE user_id=$1),0)", a.UserID).Scan(&floor); err != nil {
			return nil, err
		}
		changes, _, err := r.changes(a, syncmodel.Revision{Generation: r.generations[r.generation], Rev: floor})
		if err != nil {
			return nil, err
		}
		revs := []uint64{}
		types := []syncmodel.ChangeType{}
		gapless := true
		previous := floor
		for _, c := range changes {
			revs = append(revs, c.Rev)
			types = append(types, c.Type)
			gapless = gapless && c.Rev == previous+1
			previous = c.Rev
		}
		return map[string]any{"rev": r.cursor(a).Rev, "seq": seq, "generation": r.generations[r.generation], "revs": revs, "types": types, "gapless": gapless, "count": len(changes)}, nil
	}
	return nil, fmt.Errorf("unknown step %s: %w", step.Op, ErrInvalid)
}
func (r *coreVectorRunner) nextOp() string { r.serial++; return fmt.Sprintf("fixture-%d", r.serial) }

// Vector objects are fixture provisioning. Label membership mutations still use
// the native service; definitions have no separate event in the model language.
func (r *coreVectorRunner) putObject(a Actor, o syncmodel.Object) (syncmodel.Object, error) {
	ctx := context.Background()
	key := string(o.Kind) + "/" + strconv.FormatInt(int64(o.ID), 10)
	if o.Kind == syncmodel.LabelObject {
		if _, err := r.f.db.Pool().Exec(ctx, "INSERT INTO labels(id,user_id,name,rev) VALUES($1,$2,$3,1) ON CONFLICT(id) DO NOTHING", int64(o.ID), a.UserID, o.Name); err != nil {
			return o, err
		}
		if _, err := r.f.db.Pool().Exec(ctx, `INSERT INTO entry_labels(id,user_id,entry_id,label_id,rev,deleted_at) VALUES($1,$2,$3,$1,1,$4) ON CONFLICT(user_id,entry_id,label_id) DO NOTHING`, int64(o.ID), a.UserID, int64(o.EntryID), r.f.s.now()); err != nil {
			return o, err
		}
		var membership LabelMembership
		var err error
		if o.Deleted {
			membership, err = r.f.s.RemoveLabel(ctx, a, int64(o.ID), int64(o.EntryID), r.nextOp())
		} else {
			membership, err = r.f.s.AddLabel(ctx, a, int64(o.ID), int64(o.EntryID), r.nextOp())
		}
		o.Rev = membership.Rev
		if err == nil {
			r.objects[key] = o
		}
		return o, err
	}
	if o.Kind == syncmodel.SubscriptionObject {
		// Scope fixture provisioning must not emit an extra setup event.
		if o.FolderID != 0 {
			if _, err := r.f.db.Pool().Exec(ctx, "INSERT INTO folders(id,user_id,name) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING", int64(o.FolderID), a.UserID, "Fixture "+strconv.FormatInt(int64(o.FolderID), 10)); err != nil {
				return o, err
			}
		}
		err := r.f.db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := store.LockCore(ctx, tx); err != nil {
				return err
			}
			kind := syncmodel.SubscriptionCreated
			if _, exists := r.objects[key]; exists {
				kind = syncmodel.SubscriptionUpdated
			}
			if o.Deleted {
				kind = syncmodel.SubscriptionDeleted
			}
			current, err := stateRevision(ctx, tx, a.UserID)
			if err != nil {
				return err
			}
			o.Rev = current.Rev + 1
			var folder any
			if o.FolderID != 0 {
				folder = int64(o.FolderID)
			}
			var deleted any
			if o.Deleted {
				deleted = r.f.s.now()
			}
			if _, err := tx.Exec(ctx, "UPDATE subscriptions SET folder_id=$2,title_override=$3,rev=$4,deleted_at=$5 WHERE id=$1 AND user_id=$6", int64(o.ID), folder, o.Name, int64(o.Rev), deleted, a.UserID); err != nil {
				return err
			}
			_, err = r.f.s.change(ctx, tx, a.UserID, kind, map[string]any{"object": o})
			return err
		})
		if err == nil {
			r.objects[key] = o
		}
		return o, err
	}
	return o, ErrInvalid
}

var coreSnapshotTables = []string{"entry_state", "op_ids", "changes", "entry_revisions", "positions", "rev_counters", "seq_counter"}

func (r *coreVectorRunner) snapshot() map[string][]json.RawMessage {
	out := map[string][]json.RawMessage{}
	for _, table := range coreSnapshotTables {
		rows, err := r.f.db.Pool().Query(context.Background(), "SELECT row_to_json(t) FROM "+pgx.Identifier{table}.Sanitize()+" t")
		if err != nil {
			r.t.Fatal(err)
		}
		for rows.Next() {
			var body json.RawMessage
			if err := rows.Scan(&body); err != nil {
				r.t.Fatal(err)
			}
			out[table] = append(out[table], body)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			r.t.Fatal(err)
		}
	}
	return out
}
func (r *coreVectorRunner) restore(snapshot map[string][]json.RawMessage) error {
	return r.f.db.WithTx(context.Background(), func(tx pgx.Tx) error {
		for _, table := range coreSnapshotTables {
			name := pgx.Identifier{table}.Sanitize()
			if _, err := tx.Exec(context.Background(), "DELETE FROM "+name); err != nil {
				return err
			}
			for _, body := range snapshot[table] {
				if _, err := tx.Exec(context.Background(), "INSERT INTO "+name+" SELECT * FROM json_populate_record(NULL::"+name+",$1::json)", []byte(body)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func TestCoreVectorUnknownRejected(t *testing.T) {
	if slices.Contains(coreVectorFiles, "99-unknown.json") {
		t.Fatal("unknown vector accepted")
	}
	r := &coreVectorRunner{}
	if _, err := r.execute(coreStep{Op: "unregistered"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown operation accepted", err)
	}
	// Syntax validation precedes database work in the fixture decoder.
	var step coreStep
	d := json.NewDecoder(strings.NewReader(`{"op":"write","unknown":true}`))
	d.DisallowUnknownFields()
	if err := d.Decode(&step); err == nil {
		t.Fatal("unknown field accepted")
	}
}
