package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// mediaPublisher serves a home page, an icon and article images, counting
// requests per path. Paths in failing answer with that status instead.
type mediaPublisher struct {
	*httptest.Server
	mu      sync.Mutex
	hits    map[string]int
	failing map[string]int
}

func testImage(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&buf, img, nil)
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newMediaPublisher(t *testing.T) *mediaPublisher {
	t.Helper()
	p := &mediaPublisher{hits: map[string]int{}, failing: map[string]int{}}
	files := map[string]struct {
		kind string
		body []byte
	}{
		"/":           {"text/html", []byte(`<!doctype html><html><head><link rel="icon" sizes="32x32" href="/icon.png"></head><body></body></html>`)},
		"/icon.png":   {"image/png", testImage(t, "png", 32, 32)},
		"/photo.jpg":  {"image/jpeg", testImage(t, "jpeg", 200, 120)},
		"/second.jpg": {"image/jpeg", testImage(t, "jpeg", 300, 300)},
		"/tiny.png":   {"image/png", testImage(t, "png", 10, 10)},
		"/flaky.jpg":  {"image/jpeg", testImage(t, "jpeg", 100, 100)},
	}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.hits[r.URL.Path]++
		status := p.failing[r.URL.Path]
		p.mu.Unlock()
		file, ok := files[r.URL.Path]
		if status != 0 || !ok {
			if status == 0 {
				status = http.StatusNotFound
			}
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", file.kind)
		_, _ = w.Write(file.body)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *mediaPublisher) hit(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits[path]
}

func (p *mediaPublisher) fail(path string, status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failing[path] = status
}

// mediaTestNew is entriesTestNew with an outbound client that may reach the
// publisher on its loopback port.
func mediaTestNew(t *testing.T, template *storetest.Template, p *mediaPublisher) *entriesTestFixture {
	t.Helper()
	f := entriesTestNew(t, template)
	u, _ := url.Parse(p.URL)
	_, rawPort, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(rawPort)
	client, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f.s, err = New(f.db, Deps{Clock: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }, SecretKey: []byte(strings.Repeat("e", 32)), Outbound: client, MediaPorts: []uint16{uint16(port)}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type mediaRow struct {
	status     string
	candidates []string
	attempts   int
	next       time.Time
	source     string
	kind       string
	body       []byte
}

func loadEntryImage(t *testing.T, f *entriesTestFixture, entry int64) (mediaRow, bool) {
	t.Helper()
	var r mediaRow
	var source, kind *string
	err := f.db.Pool().QueryRow(context.Background(), "SELECT status,candidates,attempts,next_attempt_at,source_url,content_type,body FROM entry_images WHERE entry_id=$1", entry).Scan(&r.status, &r.candidates, &r.attempts, &r.next, &source, &kind, &r.body)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return r, false
		}
		t.Fatal(err)
	}
	if source != nil {
		r.source = *source
	}
	if kind != nil {
		r.kind = *kind
	}
	return r, true
}

func mediaItem(key, image, html string) IngestItem {
	item := entriesTestItem(key)
	item.Image = image
	if html != "" {
		item.ContentHTML = html
	}
	return item
}

func (f *entriesTestFixture) record(t *testing.T, feed int64, meta *FeedMeta, items ...IngestItem) {
	t.Helper()
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: feed, Status: FetchOK, Feed: meta, Items: items}); err != nil {
		t.Fatal(err)
	}
}

func (f *entriesTestFixture) entryID(t *testing.T, feed int64, key string) int64 {
	t.Helper()
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT id FROM entries WHERE feed_id=$1 AND identity_hash=$2", feed, entriesTestHashB("id:"+key)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *entriesTestFixture) runMedia(t *testing.T) int {
	t.Helper()
	n, err := f.s.RunMedia(context.Background(), deliveryBatchForTest)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

const deliveryBatchForTest = 100

func TestMedia(t *testing.T) {
	template := storetest.NewTemplate(t)

	t.Run("ingest, fetch, list and serve", func(t *testing.T) {
		p := newMediaPublisher(t)
		f := mediaTestNew(t, template, p)
		ctx := context.Background()
		feed := entriesTestFeed(t, f.db, p.URL+"/feed.xml", 0)
		sub := entriesTestSub(t, f.db, f.a.UserID, feed, "now")
		p.fail("/gone.jpg", http.StatusNotFound)
		f.record(t, feed, &FeedMeta{Title: "Publisher", SiteURL: p.URL + "/"},
			mediaItem("declared", p.URL+"/photo.jpg", ""),
			mediaItem("inline", "", `<p><img src="`+p.URL+`/tiny.png"><img src="`+p.URL+`/second.jpg"></p>`),
			mediaItem("gone", p.URL+"/gone.jpg", ""),
			mediaItem("plain", "", ""),
		)
		declared, inline, gone, plain := f.entryID(t, feed, "declared"), f.entryID(t, feed, "inline"), f.entryID(t, feed, "gone"), f.entryID(t, feed, "plain")
		if r, _ := loadEntryImage(t, f, declared); r.status != mediaPending || len(r.candidates) != 1 || r.candidates[0] != p.URL+"/photo.jpg" {
			t.Fatalf("declared row=%+v", r)
		}
		if r, _ := loadEntryImage(t, f, inline); r.status != mediaPending || len(r.candidates) != 2 {
			t.Fatalf("inline row=%+v", r)
		}
		if r, _ := loadEntryImage(t, f, plain); r.status != mediaNone || len(r.candidates) != 0 {
			t.Fatalf("plain row=%+v", r)
		}
		// Nothing is fetched until the worker runs, and listing fetches nothing.
		page, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			if e.ThumbnailURL != "" {
				t.Fatalf("thumbnail before fetch: %+v", e)
			}
		}
		if p.hit("/photo.jpg") != 0 || p.hit("/") != 0 {
			t.Fatalf("fetched at ingest or list time: %v", p.hits)
		}

		if n := f.runMedia(t); n == 0 {
			t.Fatal("media pass did nothing")
		}
		r, _ := loadEntryImage(t, f, declared)
		if r.status != mediaReady || r.kind != "image/jpeg" || len(r.body) == 0 || r.source != p.URL+"/photo.jpg" || r.attempts != 0 {
			t.Fatalf("declared after pass=%+v", r)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(r.body))
		if err != nil || cfg.Width != 120 || cfg.Height != 120 {
			t.Fatalf("thumbnail %dx%d: %v", cfg.Width, cfg.Height, err)
		}
		// The tiny inline image is unsuitable, so the next candidate is used.
		if r, _ := loadEntryImage(t, f, inline); r.status != mediaReady || r.source != p.URL+"/second.jpg" {
			t.Fatalf("inline after pass=%+v", r)
		}
		if r, _ := loadEntryImage(t, f, gone); r.status != mediaFailed || r.body != nil {
			t.Fatalf("gone after pass=%+v", r)
		}
		var iconStatus, iconSource string
		if err := f.db.Pool().QueryRow(ctx, "SELECT status,source_url FROM feed_icons WHERE feed_id=$1", feed).Scan(&iconStatus, &iconSource); err != nil {
			t.Fatal(err)
		}
		if iconStatus != mediaReady || iconSource != p.URL+"/icon.png" {
			t.Fatalf("icon status=%s source=%s", iconStatus, iconSource)
		}

		page, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll})
		if err != nil {
			t.Fatal(err)
		}
		withThumb := map[int64]string{}
		for _, e := range page {
			if e.ThumbnailURL != "" {
				withThumb[e.ID] = e.ThumbnailURL
			}
		}
		if len(withThumb) != 2 || !strings.HasPrefix(withThumb[declared], "/v1/entries/"+strconv.FormatInt(declared, 10)+"/thumbnail?v=") {
			t.Fatalf("list thumbnails=%v", withThumb)
		}
		one, err := f.s.Entry(ctx, f.a, declared)
		if err != nil || one.ThumbnailURL != withThumb[declared] {
			t.Fatalf("entry thumbnail=%q: %v", one.ThumbnailURL, err)
		}
		byIDs, err := f.s.GetByIDs(ctx, f.a, []int64{inline})
		if err != nil || len(byIDs) != 1 || byIDs[0].ThumbnailURL != withThumb[inline] {
			t.Fatalf("by ids=%+v: %v", byIDs, err)
		}
		file, err := f.s.EntryThumbnail(ctx, f.a, declared)
		if err != nil || file.ContentType != "image/jpeg" || !strings.HasSuffix(withThumb[declared], "?v="+file.ETag) {
			t.Fatalf("thumbnail file=%s %s: %v", file.ContentType, file.ETag, err)
		}
		if _, err := f.s.EntryThumbnail(ctx, f.a, plain); !errors.Is(err, ErrNotFound) {
			t.Fatalf("plain thumbnail err=%v", err)
		}
		subs, err := f.s.Subscriptions(ctx, f.a)
		if err != nil || len(subs) != 1 || !strings.HasPrefix(subs[0].IconURL, "/v1/subscriptions/"+strconv.FormatInt(sub, 10)+"/icon?v=") {
			t.Fatalf("subscriptions=%+v: %v", subs, err)
		}
		icon, err := f.s.SubscriptionIcon(ctx, f.a, sub)
		if err != nil || icon.ContentType != "image/png" {
			t.Fatalf("icon=%s: %v", icon.ContentType, err)
		}
		if cfg, err := png.DecodeConfig(bytes.NewReader(icon.Body)); err != nil || cfg.Width != 32 || cfg.Height != 32 {
			t.Fatalf("icon %dx%d: %v", cfg.Width, cfg.Height, err)
		}

		// Another reader sees neither.
		var otherUser, otherPrincipal int64
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('mediaOther','$argon2id$test') RETURNING id").Scan(&otherUser); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id", otherUser).Scan(&otherPrincipal); err != nil {
			t.Fatal(err)
		}
		other := Actor{UserID: otherUser, PrincipalID: otherPrincipal, Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}
		if _, err := f.s.EntryThumbnail(ctx, other, declared); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other thumbnail err=%v", err)
		}
		if _, err := f.s.SubscriptionIcon(ctx, other, sub); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other icon err=%v", err)
		}

		// Recording the same items again leaves stored thumbnails alone.
		before := p.hit("/photo.jpg")
		f.record(t, feed, &FeedMeta{Title: "Publisher", SiteURL: p.URL + "/"}, mediaItem("declared", p.URL+"/photo.jpg", ""))
		f.runMedia(t)
		if p.hit("/photo.jpg") != before {
			t.Fatal("an unchanged item was fetched again")
		}

		// A new version with another image starts over; the old thumbnail
		// stays servable until the new one fails or replaces it.
		changed := mediaItem("declared", p.URL+"/gone.jpg", "<p>new body</p>")
		changed.ContentHash = entriesTestHash("body:declared-v2")
		f.record(t, feed, &FeedMeta{Title: "Publisher", SiteURL: p.URL + "/"}, changed)
		if r, _ := loadEntryImage(t, f, declared); r.status != mediaPending || r.attempts != 0 || len(r.body) == 0 {
			t.Fatalf("changed row=%+v", r)
		}
		f.runMedia(t)
		if r, _ := loadEntryImage(t, f, declared); r.status != mediaFailed || len(r.body) == 0 {
			t.Fatalf("changed after pass=%+v", r)
		}
		if one, err := f.s.Entry(ctx, f.a, declared); err != nil || one.ThumbnailURL == "" {
			t.Fatalf("old thumbnail dropped: %+v %v", one, err)
		}

		// Retention removes the thumbnail with the body.
		if _, err := f.s.RetainEntries(ctx, f.now().Add(400*24*time.Hour), time.Hour); err != nil {
			t.Fatal(err)
		}
		if _, ok := loadEntryImage(t, f, declared); ok {
			t.Fatal("retention kept the thumbnail")
		}
	})

	t.Run("transient failures retry with backoff, then give up", func(t *testing.T) {
		p := newMediaPublisher(t)
		f := mediaTestNew(t, template, p)
		feed := entriesTestFeed(t, f.db, p.URL+"/feed.xml", 0)
		entriesTestSub(t, f.db, f.a.UserID, feed, "now")
		p.fail("/flaky.jpg", http.StatusServiceUnavailable)
		f.record(t, feed, nil, mediaItem("flaky", p.URL+"/flaky.jpg", ""), mediaItem("later", p.URL+"/flaky.jpg?later", ""))
		flaky, later := f.entryID(t, feed, "flaky"), f.entryID(t, feed, "later")
		start := f.now()
		f.runMedia(t)
		r, _ := loadEntryImage(t, f, flaky)
		if r.status != mediaPending || r.attempts != 1 || !r.next.Equal(start.Add(mediaRetryBase)) {
			t.Fatalf("after first failure=%+v", r)
		}
		// Not due yet: a pass now must not refetch it.
		hits := p.hit("/flaky.jpg")
		f.runMedia(t)
		if p.hit("/flaky.jpg") != hits {
			t.Fatal("retried before the backoff elapsed")
		}
		f.setClock(start.Add(mediaRetryBase + time.Second))
		f.runMedia(t)
		if r, _ := loadEntryImage(t, f, flaky); r.status != mediaPending || r.attempts != 2 {
			t.Fatalf("after second failure=%+v", r)
		}
		// The third transient failure is final.
		f.setClock(start.Add(mediaRetryBase*3 + 2*time.Second))
		f.runMedia(t)
		for _, id := range []int64{flaky, later} {
			if r, _ := loadEntryImage(t, f, id); r.status != mediaFailed || r.attempts != 0 {
				t.Fatalf("after third failure=%+v", r)
			}
		}
		if hits := p.hit("/flaky.jpg"); hits != 6 {
			t.Fatalf("fetched %d times, want 3 per entry", hits)
		}
	})

	t.Run("backfill and icon seeding cover data from before the upgrade", func(t *testing.T) {
		p := newMediaPublisher(t)
		f := mediaTestNew(t, template, p)
		ctx := context.Background()
		feed := entriesTestFeed(t, f.db, p.URL+"/feed.xml", 0)
		entriesTestSub(t, f.db, f.a.UserID, feed, "now")
		base := f.now()
		f.setClock(base.Add(-8 * 24 * time.Hour))
		f.record(t, feed, nil, mediaItem("old", "", `<img src="`+p.URL+`/photo.jpg?old">`))
		f.setClock(base)
		f.record(t, feed, nil, mediaItem("recent", "", `<img src="`+p.URL+`/photo.jpg?recent">`))
		old, recent := f.entryID(t, feed, "old"), f.entryID(t, feed, "recent")
		// Simulate rows written before migration 0011.
		if _, err := f.db.Pool().Exec(ctx, "DELETE FROM entry_images; DELETE FROM feed_icons"); err != nil {
			t.Fatal(err)
		}
		f.runMedia(t)
		if _, ok := loadEntryImage(t, f, old); ok {
			t.Fatal("backfilled an entry outside the window")
		}
		if r, ok := loadEntryImage(t, f, recent); !ok || r.status != mediaReady || r.source != p.URL+"/photo.jpg?recent" {
			t.Fatalf("recent=%+v ok=%v", r, ok)
		}
		// The feed never named a site, so the icon comes from its origin.
		var status string
		if err := f.db.Pool().QueryRow(ctx, "SELECT status FROM feed_icons WHERE feed_id=$1", feed).Scan(&status); err != nil || status != mediaReady {
			t.Fatalf("seeded icon status=%q: %v", status, err)
		}
		if !f.s.mediaBackfilled.Load() {
			f.runMedia(t)
		}
		if !f.s.mediaBackfilled.Load() {
			t.Fatal("backfill never finished")
		}
	})

	t.Run("feeds without subscribers are left alone", func(t *testing.T) {
		p := newMediaPublisher(t)
		f := mediaTestNew(t, template, p)
		feed := entriesTestFeed(t, f.db, p.URL+"/feed.xml", 0)
		f.record(t, feed, &FeedMeta{Title: "Nobody", SiteURL: p.URL + "/"})
		f.runMedia(t)
		if p.hit("/") != 0 || p.hit("/icon.png") != 0 {
			t.Fatalf("fetched for an unsubscribed feed: %v", p.hits)
		}
	})

	t.Run("changed icon inputs reschedule resolution", func(t *testing.T) {
		p := newMediaPublisher(t)
		f := mediaTestNew(t, template, p)
		ctx := context.Background()
		feed := entriesTestFeed(t, f.db, p.URL+"/feed.xml", 0)
		entriesTestSub(t, f.db, f.a.UserID, feed, "now")
		meta := &FeedMeta{Title: "Publisher", SiteURL: p.URL + "/"}
		f.record(t, feed, meta)
		f.runMedia(t)
		var next time.Time
		if err := f.db.Pool().QueryRow(ctx, "SELECT next_attempt_at FROM feed_icons WHERE feed_id=$1", feed).Scan(&next); err != nil || !next.Equal(f.now().Add(iconRefresh)) {
			t.Fatalf("refresh at %v: %v", next, err)
		}
		f.record(t, feed, meta)
		if err := f.db.Pool().QueryRow(ctx, "SELECT next_attempt_at FROM feed_icons WHERE feed_id=$1", feed).Scan(&next); err != nil || !next.Equal(f.now().Add(iconRefresh)) {
			t.Fatalf("unchanged inputs rescheduled: %v %v", next, err)
		}
		f.record(t, feed, &FeedMeta{Title: "Publisher", SiteURL: p.URL + "/", IconURL: p.URL + "/icon.png"})
		if err := f.db.Pool().QueryRow(ctx, "SELECT next_attempt_at FROM feed_icons WHERE feed_id=$1", feed).Scan(&next); err != nil || !next.Equal(f.now()) {
			t.Fatalf("changed inputs not rescheduled: %v %v", next, err)
		}
	})
}
