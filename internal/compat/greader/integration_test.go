package greader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func TestClientSync(t *testing.T) {
	ctx := context.Background()
	db := storetest.NewTemplate(t).New(t)
	key := []byte(strings.Repeat("a", 32))
	auth := service.NewAuth(db, nil, key)
	actor, err := auth.CreateUser(ctx, "reader", "password")
	if err != nil {
		t.Fatal(err)
	}
	password, err := auth.SetCompatPassword(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(db, service.Deps{SecretKey: key})
	if err != nil {
		t.Fatal(err)
	}
	var feedID int64
	if err = db.Pool().QueryRow(ctx, "INSERT INTO feeds(url,title) VALUES('https://example.com/feed','Feed') RETURNING id").Scan(&feedID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", actor.UserID, feedID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, `INSERT INTO entries(feed_id,seq,identity_hash,title) SELECT $1,n,decode(md5(n::text)||md5(n::text),'hex'),'Item' FROM generate_series(1,10001) n`, feedID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, "UPDATE seq_counter SET value=10001 WHERE singleton"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(svc, auth, nil))
	defer server.Close()
	request := func(method, path string, form url.Values, token string) *http.Response {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, e := http.NewRequest(method, server.URL+prefix+path, body)
		if e != nil {
			t.Fatal(e)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if token != "" {
			req.Header.Set("Authorization", "GoogleLogin auth="+token)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	read := func(resp *http.Response) string {
		t.Helper()
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("status %d: %s", resp.StatusCode, b)
		}
		return string(b)
	}
	login := read(request("POST", "accounts/ClientLogin", url.Values{"Email": {"reader"}, "Passwd": {password}}, ""))
	var token string
	for _, line := range strings.Split(login, "\n") {
		if v, ok := strings.CutPrefix(line, "Auth="); ok {
			token = v
		}
	}
	if token == "" {
		t.Fatal(login)
	}
	csrfToken := read(request("GET", api+"token", nil, token))
	for _, path := range []string{"tag/list", "subscription/list"} {
		read(request("GET", api+path, nil, token))
	}

	// Clients revalidate both lists using ETags.
	for _, path := range []string{"tag/list", "subscription/list"} {
		resp := request("GET", api+path, nil, token)
		tag := resp.Header.Get("ETag")
		resp.Body.Close()
		req, err := http.NewRequest("GET", server.URL+prefix+api+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "GoogleLogin auth="+token)
		req.Header.Set("If-None-Match", tag)
		cached, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		cached.Body.Close()
		if tag == "" || cached.StatusCode != http.StatusNotModified {
			t.Fatalf("%s: ETag %q status %d", path, tag, cached.StatusCode)
		}
	}
	denied := request("POST", api+"edit-tag", url.Values{"i": {"1"}, "a": {statePrefix + "read"}}, token)
	denied.Body.Close()
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing T: %d", denied.StatusCode)
	}
	var page struct {
		ItemRefs []struct {
			ID string `json:"id"`
		} `json:"itemRefs"`
		Continuation string `json:"continuation"`
	}
	first := read(request("GET", api+"stream/items/ids?n=10000&xt="+url.QueryEscape(statePrefix+"read"), nil, token))
	if err = json.Unmarshal([]byte(first), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.ItemRefs) != 10000 || page.Continuation == "" {
		t.Fatalf("first page %d %q", len(page.ItemRefs), page.Continuation)
	}
	firstID := page.ItemRefs[0].ID
	var secondID int64
	if err := db.Pool().QueryRow(ctx, "SELECT id FROM entries WHERE id <> $1::bigint AND seq=3000", firstID).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {strconv.FormatInt(secondID, 10)}, "a": {labelPrefix + "mixed"}}, token))
	read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {firstID, strconv.FormatInt(secondID, 10)}, "r": {labelPrefix + "mixed"}}, token))
	var remains bool
	if err := db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entry_labels el JOIN labels l ON l.id=el.label_id WHERE el.entry_id=$1 AND l.name='mixed' AND el.deleted_at IS NULL)`, secondID).Scan(&remains); err != nil || remains {
		t.Fatalf("label remains: %v %v", remains, err)
	}
	read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {strconv.FormatInt(secondID, 10)}, "a": {labelPrefix + "mixed"}}, token))
	read(request("POST", api+"mark-all-as-read", url.Values{"T": {csrfToken}, "s": {labelPrefix + "mixed"}}, token))
	var labelSeen string
	if err := db.Pool().QueryRow(ctx, "SELECT seen FROM entry_state WHERE principal_id=$1 AND entry_id=$2", actor.PrincipalID, secondID).Scan(&labelSeen); err != nil || labelSeen != "seen" {
		t.Fatalf("label mark-all: %s %v", labelSeen, err)
	}
	read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {strconv.FormatInt(secondID, 10)}, "r": {statePrefix + "read"}}, token))
	next := read(request("GET", api+"stream/items/ids?n=10000&xt="+url.QueryEscape(statePrefix+"read")+"&c="+page.Continuation, nil, token))
	page.ItemRefs = nil
	if err = json.Unmarshal([]byte(next), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.ItemRefs) != 1 {
		t.Fatalf("second page %d", len(page.ItemRefs))
	}
	content := read(request("POST", api+"stream/items/contents", url.Values{"i": {firstID}}, token))
	if !strings.Contains(content, itemPrefix) {
		t.Fatal(content)
	}
	for _, tc := range []struct{ key, tag string }{{"a", "read"}, {"r", "read"}, {"a", "starred"}, {"r", "starred"}} {
		form := url.Values{"T": {csrfToken}, "i": {firstID}, tc.key: {statePrefix + tc.tag}}
		if got := read(request("POST", api+"edit-tag", form, token)); got != "OK" {
			t.Fatal(got)
		}
	}
	var seen, shelf string
	var kept bool
	if err = db.Pool().QueryRow(ctx, "SELECT seen,shelf,kept FROM entry_state WHERE principal_id=$1 AND entry_id=$2", actor.PrincipalID, firstID).Scan(&seen, &shelf, &kept); err != nil {
		t.Fatal(err)
	}
	if seen != "unseen" || shelf != "none" || kept {
		t.Fatal(fmt.Sprint(seen, shelf, kept))
	}
	// A compatibility unread clears Done (vector 16).
	cursor, err := svc.BootstrapCursor(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	id, err := strconv.ParseInt(firstID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Batch(ctx, actor, []syncmodel.Write{{OpID: "native-done", Principal: syncmodel.ID(actor.PrincipalID), EntryID: syncmodel.ID(id), Register: syncmodel.Shelf, Value: syncmodel.Done, Base: syncmodel.Revision{Generation: cursor.Generation}, OccurredAt: time.Now(), Kind: syncmodel.Explicit}})
	if err != nil {
		t.Fatal(err)
	}
	read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {firstID}, "r": {statePrefix + "read"}}, token))
	if err = db.Pool().QueryRow(ctx, "SELECT seen,shelf FROM entry_state WHERE principal_id=$1 AND entry_id=$2", actor.PrincipalID, id).Scan(&seen, &shelf); err != nil {
		t.Fatal(err)
	}
	if seen != "unseen" || shelf != "none" {
		t.Fatalf("unread Done: %s %s", seen, shelf)
	}
	// A later passive read cannot override deliberate unseen (vectors 12, 20).
	read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {firstID}, "a": {statePrefix + "read"}}, token))
	if err = db.Pool().QueryRow(ctx, "SELECT seen FROM entry_state WHERE principal_id=$1 AND entry_id=$2", actor.PrincipalID, id).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != "unseen" {
		t.Fatalf("passive read won: %s", seen)
	}

	// A later compatibility star and unstar beat older deliberate native actions (vector 20).
	for _, tc := range []struct {
		native syncmodel.Value
		action string
		want   bool
	}{{syncmodel.False, "a", true}, {syncmodel.True, "r", false}} {
		_, err = svc.Batch(ctx, actor, []syncmodel.Write{{OpID: "native-kept-" + tc.action, Principal: syncmodel.ID(actor.PrincipalID), EntryID: syncmodel.ID(id), Register: syncmodel.Kept, Value: tc.native, Base: syncmodel.Revision{Generation: cursor.Generation}, OccurredAt: time.Now().Add(-time.Second), Kind: syncmodel.Explicit}})
		if err != nil {
			t.Fatal(err)
		}
		read(request("POST", api+"edit-tag", url.Values{"T": {csrfToken}, "i": {firstID}, tc.action: {statePrefix + "starred"}}, token))
		if err = db.Pool().QueryRow(ctx, "SELECT kept FROM entry_state WHERE principal_id=$1 AND entry_id=$2", actor.PrincipalID, id).Scan(&kept); err != nil {
			t.Fatal(err)
		}
		if kept != tc.want {
			t.Fatalf("%s star: %v", tc.action, kept)
		}
	}

	// Vector 12: a burst of 2,000 old read marks must leave deliberate unreads intact.
	if _, err = db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,seen,seen_rev,seen_occurred_at,seen_actor,seen_source,state_rev)
 SELECT $1,id,'unseen',1,now(),$1,'deliberate',1 FROM entries WHERE seq<=2000`, actor.PrincipalID); err != nil {
		t.Fatal(err)
	}
	burst := url.Values{"T": {csrfToken}, "a": {statePrefix + "read"}}
	for i := 1; i <= 2000; i++ {
		burst.Add("i", strconv.Itoa(i))
	}
	read(request("POST", api+"edit-tag", burst, token))
	var unread int
	if err = db.Pool().QueryRow(ctx, "SELECT count(*) FROM entry_state WHERE principal_id=$1 AND entry_id<=2000 AND seen='unseen'", actor.PrincipalID).Scan(&unread); err != nil {
		t.Fatal(err)
	}
	if unread != 2000 {
		t.Fatalf("deliberate unreads retained: %d", unread)
	}

	// A bulk tag edit over several hundred items runs as one service op per
	// direction: one op record, one transaction, per-membership events.
	var opsBefore int
	if err = db.Pool().QueryRow(ctx, `SELECT count(*) FROM op_ids WHERE principal_id=$1 AND result->>'method'='AddLabels'`, actor.PrincipalID).Scan(&opsBefore); err != nil {
		t.Fatal(err)
	}
	bulk := url.Values{"T": {csrfToken}, "a": {labelPrefix + "bulk"}}
	for i := 6001; i <= 6500; i++ {
		bulk.Add("i", strconv.Itoa(i))
	}
	if got := read(request("POST", api+"edit-tag", bulk, token)); got != "OK" {
		t.Fatal(got)
	}
	var labeled int
	if err = db.Pool().QueryRow(ctx, `SELECT count(*) FROM entry_labels el JOIN labels l ON l.id=el.label_id AND l.user_id=el.user_id WHERE el.user_id=$1 AND l.name='bulk' AND el.deleted_at IS NULL`, actor.UserID).Scan(&labeled); err != nil || labeled != 500 {
		t.Fatalf("bulk add: %d %v", labeled, err)
	}
	var bulkLabel int64
	if err = db.Pool().QueryRow(ctx, "SELECT id FROM labels WHERE user_id=$1 AND name='bulk'", actor.UserID).Scan(&bulkLabel); err != nil {
		t.Fatal(err)
	}
	var addOps, addEvents int
	if err = db.Pool().QueryRow(ctx, `SELECT count(*) FROM op_ids WHERE principal_id=$1 AND result->>'method'='AddLabels'`, actor.PrincipalID).Scan(&addOps); err != nil || addOps != opsBefore+1 {
		t.Fatalf("bulk add ops: %d (before %d) %v", addOps, opsBefore, err)
	}
	if err = db.Pool().QueryRow(ctx, `SELECT count(*) FROM changes WHERE user_id=$1 AND type='label.added' AND (payload->>'label_id')::bigint=$2`, actor.UserID, bulkLabel).Scan(&addEvents); err != nil || addEvents != 500 {
		t.Fatalf("bulk add events: %d %v", addEvents, err)
	}
	// A mixed add stops at the first invalid id with the prefix committed,
	// as repeated per-entry edits did before.
	mixed := url.Values{"T": {csrfToken}, "a": {labelPrefix + "bulk"}}
	for i := 7001; i <= 7050; i++ {
		mixed.Add("i", strconv.Itoa(i))
	}
	mixed.Add("i", "99999999")
	for i := 7051; i <= 7100; i++ {
		mixed.Add("i", strconv.Itoa(i))
	}
	resp := request("POST", api+"edit-tag", mixed, token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mixed add: %d", resp.StatusCode)
	}
	if err = db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM entry_labels el WHERE el.user_id=$1 AND el.label_id=$2 AND el.deleted_at IS NULL AND el.entry_id BETWEEN 7001 AND 7050),(SELECT count(*) FROM entry_labels el WHERE el.user_id=$1 AND el.label_id=$2 AND el.deleted_at IS NULL AND el.entry_id BETWEEN 7051 AND 7100)`, actor.UserID, bulkLabel).Scan(&labeled, &addEvents); err != nil || labeled != 50 || addEvents != 0 {
		t.Fatalf("mixed prefix: %d/%d %v", labeled, addEvents, err)
	}
	// A bulk remove skips missing memberships and still reports OK.
	bulk.Del("a")
	bulk.Add("r", labelPrefix+"bulk")
	bulk.Add("i", "99999998")
	if got := read(request("POST", api+"edit-tag", bulk, token)); got != "OK" {
		t.Fatal(got)
	}
	if err = db.Pool().QueryRow(ctx, `SELECT count(*) FROM entry_labels el WHERE el.user_id=$1 AND el.label_id=$2 AND el.deleted_at IS NULL AND el.entry_id BETWEEN 6001 AND 6500`, actor.UserID, bulkLabel).Scan(&labeled); err != nil || labeled != 0 {
		t.Fatalf("bulk remove: %d %v", labeled, err)
	}

	// An omitted timestamp captures the current sequence boundary.
	var otherFeed, otherID int64
	if err = db.Pool().QueryRow(ctx, "INSERT INTO feeds(url) VALUES('https://example.com/other') RETURNING id").Scan(&otherFeed); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", actor.UserID, otherFeed); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool().QueryRow(ctx, "INSERT INTO entries(feed_id,seq,identity_hash) VALUES($1,10002,decode(md5('other')||md5('other'),'hex')) RETURNING id", otherFeed).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, "UPDATE seq_counter SET value=10002 WHERE singleton"); err != nil {
		t.Fatal(err)
	}
	read(request("POST", api+"mark-all-as-read", url.Values{"T": {csrfToken}, "s": {"feed/https://example.com/other"}}, token))
	if err = db.Pool().QueryRow(ctx, "SELECT seen FROM entry_state WHERE principal_id=$1 AND entry_id=$2", actor.PrincipalID, otherID).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != "seen" {
		t.Fatalf("mark-all: %s", seen)
	}

}

// TestStreamFilters exercises stream/items/ids and stream/items/contents over
// a corpus larger than the 200-entry fetch page, comparing every response
// against a reference implementation of the pre-optimization in-memory
// filtering (whole-membership bootstrap plus sequential scan).
func TestStreamFilters(t *testing.T) {
	ctx := context.Background()
	db := storetest.NewTemplate(t).New(t)
	key := []byte(strings.Repeat("b", 32))
	auth := service.NewAuth(db, nil, key)
	actor, err := auth.CreateUser(ctx, "stream", "password")
	if err != nil {
		t.Fatal(err)
	}
	password, err := auth.SetCompatPassword(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(db, service.Deps{SecretKey: key})
	if err != nil {
		t.Fatal(err)
	}
	var folderID, feedA, feedB int64
	if err := db.Pool().QueryRow(ctx, "INSERT INTO folders(user_id,name,rev) VALUES($1,'News',1) RETURNING id", actor.UserID).Scan(&folderID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO feeds(url,title) VALUES('https://example.com/a','A') RETURNING id").Scan(&feedA); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO feeds(url,title) VALUES('https://example.com/b','B') RETURNING id").Scan(&feedB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id,folder_id) VALUES($1,$2,$3),($1,$4,NULL)", actor.UserID, feedA, folderID, feedB); err != nil {
		t.Fatal(err)
	}
	// seq 1..250 on feed A, 251..449 on feed B; published_at = base + seq minutes.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO entries(feed_id,seq,identity_hash,title,published_at)
		SELECT CASE WHEN n<=250 THEN $1::bigint ELSE $2::bigint END, n, decode(md5(n::text)||md5(n::text),'hex'), 'Item',
			TIMESTAMPTZ '2024-01-01 00:00:00+00' + (n||' minutes')::interval
		FROM generate_series(1,449) n`, feedA, feedB); err != nil {
		t.Fatal(err)
	}
	// seq 450 keeps a NULL published_at so entryTime falls back to first_seen_at.
	if _, err := db.Pool().Exec(ctx, "INSERT INTO entries(feed_id,seq,identity_hash,title) VALUES($1,450,decode(md5('x')||md5('x'),'hex'),'Item')", feedB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE seq_counter SET value=450 WHERE singleton"); err != nil {
		t.Fatal(err)
	}
	label, err := svc.CreateLabel(ctx, actor, "spot", "op-spot")
	if err != nil {
		t.Fatal(err)
	}
	// 230 live memberships exceed the old 200-row bootstrap page.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO entry_labels(user_id,entry_id,label_id,rev)
		SELECT $1, id, $2, 1 FROM entries WHERE seq BETWEEN 100 AND 329`, actor.UserID, label.ID); err != nil {
		t.Fatal(err)
	}
	// Two read, one Done, one starred — all inside the label range.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,seen,seen_rev,seen_occurred_at,seen_actor,seen_source,state_rev)
		SELECT $1, id, 'seen', 1, now(), $1, 'deliberate', 1 FROM entries WHERE seq IN (120,121)`, actor.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,state_rev)
		SELECT $1, id, 'done', 1, now(), $1, 'deliberate', 1 FROM entries WHERE seq = 122`, actor.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev)
		SELECT $1, id, true, 1, now(), $1, 'deliberate', 1 FROM entries WHERE seq = 123`, actor.PrincipalID); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(svc, auth, nil))
	defer server.Close()
	request := func(method, path string, form url.Values, token string) *http.Response {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, e := http.NewRequest(method, server.URL+prefix+path, body)
		if e != nil {
			t.Fatal(e)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if token != "" {
			req.Header.Set("Authorization", "GoogleLogin auth="+token)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	read := func(resp *http.Response) string {
		t.Helper()
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("status %d: %s", resp.StatusCode, b)
		}
		return string(b)
	}
	login := read(request("POST", "accounts/ClientLogin", url.Values{"Email": {"stream"}, "Passwd": {password}}, ""))
	var token string
	for _, line := range strings.Split(login, "\n") {
		if v, ok := strings.CutPrefix(line, "Auth="); ok {
			token = v
		}
	}
	if token == "" {
		t.Fatal(login)
	}
	// want replays the old algorithm: bootstrap every membership, then scan the
	// whole history backwards applying the tag and time filters in memory.
	want := func(q url.Values) (ids []string, continuation string) {
		t.Helper()
		n := 1000
		if v := q.Get("n"); v != "" {
			n, _ = strconv.Atoi(v)
		}
		var before uint64
		if v := q.Get("c"); v != "" {
			before, _ = strconv.ParseUint(v, 10, 64)
		}
		var ot, nt int64
		if v := q.Get("ot"); v != "" {
			ot, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("nt"); v != "" {
			nt, _ = strconv.ParseInt(v, 10, 64)
		}
		subs, err := svc.Subscriptions(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		folders, err := svc.Folders(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		memberNames := map[int64][]string{}
		req := service.BootstrapRequest{Kind: service.BootstrapLabelMemberships, Limit: 200}
		for {
			p, err := svc.Bootstrap(ctx, actor, req)
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range p.Items {
				var m struct {
					EntryID int64  `json:"entry_id"`
					Name    string `json:"name"`
					Deleted bool   `json:"deleted"`
				}
				if err := json.Unmarshal(b, &m); err != nil {
					t.Fatal(err)
				}
				if !m.Deleted {
					memberNames[m.EntryID] = append(memberNames[m.EntryID], m.Name)
				}
			}
			if !p.HasMore {
				break
			}
			req.AfterID = p.NextID
			req.AfterPrincipal = p.NextPrincipal
		}
		urls := map[int64]string{}
		subfolders := map[int64]int64{}
		for _, s := range subs {
			urls[s.ID] = s.URL
			subfolders[s.ID] = s.FolderID
		}
		foldernames := map[int64]string{}
		for _, f := range folders {
			foldernames[f.ID] = f.Name
		}
		matches := func(tag string, e service.Entry) bool {
			switch tag {
			case "", statePrefix + "reading-list":
				return true
			case statePrefix + "read":
				return e.State.GoogleRead()
			case statePrefix + "starred":
				return e.State.Kept.Value == syncmodel.True
			case statePrefix + "kept-unread":
				return e.State.Unread()
			}
			if strings.HasPrefix(tag, "feed/") {
				return urls[e.SubscriptionID] == strings.TrimPrefix(tag, "feed/")
			}
			if name, ok := labelName(tag); ok {
				if foldernames[subfolders[e.SubscriptionID]] == name {
					return true
				}
				for _, m := range memberNames[e.ID] {
					if m == name {
						return true
					}
				}
			}
			return false
		}
		var last uint64
		more := false
		f := service.EntryFilter{View: service.ViewAll, Limit: 200, Direction: service.PageBackward, BeforeSeq: before}
		for {
			entries, err := svc.Entries(ctx, actor, f)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				pass := true
				for _, s := range q["s"] {
					if !matches(s, e) {
						pass = false
					}
				}
				for _, s := range q["xt"] {
					if matches(s, e) {
						pass = false
					}
				}
				for _, s := range q["it"] {
					if !matches(s, e) {
						pass = false
					}
				}
				ts := entryTime(e).Unix()
				if ot != 0 && ts <= ot {
					pass = false
				}
				if nt != 0 && ts >= nt {
					pass = false
				}
				if !pass {
					continue
				}
				if len(ids) == n {
					more = true
					break
				}
				ids = append(ids, strconv.FormatInt(e.ID, 10))
				last = e.Seq
			}
			if more || len(entries) < 200 {
				break
			}
			f.BeforeSeq = entries[len(entries)-1].Seq
		}
		if more {
			continuation = strconv.FormatUint(last, 10)
		}
		return ids, continuation
	}
	get := func(q url.Values) (ids []string, continuation string) {
		t.Helper()
		var page struct {
			ItemRefs []struct {
				ID string `json:"id"`
			} `json:"itemRefs"`
			Continuation string `json:"continuation"`
		}
		if err := json.Unmarshal([]byte(read(request("GET", api+"stream/items/ids?"+q.Encode(), nil, token))), &page); err != nil {
			t.Fatal(err)
		}
		for _, r := range page.ItemRefs {
			ids = append(ids, r.ID)
		}
		return ids, page.Continuation
	}
	check := func(q url.Values) {
		t.Helper()
		wantIDs, wantCont := want(q)
		gotIDs, gotCont := get(q)
		if !slices.Equal(gotIDs, wantIDs) {
			diff := ""
			for i := 0; i < len(gotIDs) && i < len(wantIDs); i++ {
				if gotIDs[i] != wantIDs[i] {
					diff = fmt.Sprintf("first diff at %d: %s vs %s", i, gotIDs[i], wantIDs[i])
					break
				}
			}
			t.Fatalf("%s: got %d ids, want %d ids %s", q.Encode(), len(gotIDs), len(wantIDs), diff)
		}
		if gotCont != wantCont {
			t.Fatalf("%s: continuation %q, want %q", q.Encode(), gotCont, wantCont)
		}
	}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	sec := func(seq int64) string {
		return strconv.FormatInt(base+60*seq, 10)
	}
	for _, q := range []url.Values{
		{},
		{"n": {"130"}},
		{"xt": {statePrefix + "read"}},
		{"s": {statePrefix + "read"}},
		{"s": {statePrefix + "starred"}},
		{"s": {statePrefix + "kept-unread"}},
		{"s": {"feed/https://example.com/a"}},
		{"s": {"feed/https://example.com/b"}},
		{"xt": {"feed/https://example.com/a"}},
		{"s": {labelPrefix + "News"}},
		{"s": {labelPrefix + "spot"}},
		{"s": {labelPrefix + "spot"}, "xt": {statePrefix + "read"}},
		{"s": {labelPrefix + "spot"}, "it": {statePrefix + "read"}},
		{"s": {labelPrefix + "News", statePrefix + "read"}},
		{"s": {labelPrefix + "missing"}},
		{"s": {"feed/https://example.com/nope"}},
		{"xt": {statePrefix + "reading-list"}},
		{"xt": {labelPrefix + "News"}},
		{"ot": {sec(200)}},
		{"nt": {sec(300)}},
		{"ot": {sec(200)}, "nt": {sec(300)}},
		{"s": {labelPrefix + "spot"}, "ot": {sec(150)}, "nt": {sec(260)}},
		{"n": {"97"}, "s": {labelPrefix + "spot"}},
		{"n": {"130"}, "xt": {statePrefix + "read"}},
	} {
		check(q)
	}
	// Following continuations must return the same id stream as one big page.
	for _, q := range []url.Values{
		{"n": {"130"}},
		{"n": {"97"}, "s": {labelPrefix + "spot"}},
		{"n": {"60"}, "s": {labelPrefix + "spot"}, "xt": {statePrefix + "read"}},
	} {
		var all []string
		page := url.Values{}
		for k, v := range q {
			page[k] = v
		}
		for {
			ids, cont := get(page)
			all = append(all, ids...)
			if cont == "" {
				break
			}
			page.Set("c", cont)
		}
		full := url.Values{"n": {"10000"}}
		for k, v := range q {
			if k != "n" && k != "c" {
				full[k] = v
			}
		}
		wantIDs, _ := want(full)
		if !slices.Equal(all, wantIDs) {
			t.Fatalf("drain %s: got %d ids, want %d", q.Encode(), len(all), len(wantIDs))
		}
	}
	// Contents carries the folder and label names in categories without the
	// full membership bootstrap.
	bySeq := map[int64]int64{}
	rows, err := db.Pool().Query(ctx, "SELECT id,seq FROM entries WHERE seq IN (120,340,300)")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			t.Fatal(err)
		}
		bySeq[seq] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Items []struct {
			ID         string   `json:"id"`
			Categories []string `json:"categories"`
		} `json:"items"`
	}
	content := read(request("POST", api+"stream/items/contents", url.Values{"i": {
		strconv.FormatInt(bySeq[120], 10),
		strconv.FormatInt(bySeq[340], 10),
		strconv.FormatInt(bySeq[300], 10),
	}}, token))
	if err := json.Unmarshal([]byte(content), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 3 {
		t.Fatalf("items: %s", content)
	}
	wantCats := [][]string{
		{statePrefix + "reading-list", statePrefix + "read", labelPrefix + "News", labelPrefix + "spot"},
		{statePrefix + "reading-list"},
		{statePrefix + "reading-list", labelPrefix + "spot"},
	}
	for i, item := range body.Items {
		if !slices.Equal(item.Categories, wantCats[i]) {
			t.Fatalf("item %d categories %v, want %v", i, item.Categories, wantCats[i])
		}
	}
}
