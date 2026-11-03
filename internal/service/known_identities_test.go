package service

import (
	"context"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// TestKnownIdentities pins the contract the fetcher relies on for full-text
// extraction: an identity counts as known only when its live entry already
// caches an extracted version of the current content, or when it is a
// retention tombstone that must never be resurrected or re-fetched.
func TestKnownIdentities(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("extracted_and_tombstones_only", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		feedOnly := coreItem("feed-only")
		extracted := coreItem("extracted")
		extracted.ContentSource = ContentFromExtracted
		tombstone := coreItem("tombstoned")
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{feedOnly, extracted, tombstone}}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=$2 WHERE feed_id=$1 AND identity_hash=$3", f.feed, f.s.now(), mustCoreHash(t, tombstone.IdentityHash)); err != nil {
			t.Fatal(err)
		}
		absent := coreHash("absent")
		known, err := f.s.KnownIdentities(ctx, f.feed, []string{feedOnly.IdentityHash, extracted.IdentityHash, tombstone.IdentityHash, absent})
		if err != nil {
			t.Fatal(err)
		}
		if known[feedOnly.IdentityHash] {
			t.Error("feed-sourced identity reported known; extraction would never retry")
		}
		if !known[extracted.IdentityHash] {
			t.Error("extracted identity reported unknown; cached extraction is not reused")
		}
		if !known[tombstone.IdentityHash] {
			t.Error("tombstone reported unknown; refresh could resurrect it")
		}
		if known[absent] {
			t.Error("absent identity reported known")
		}
	})
	t.Run("current_content_not_extracted_retries", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		item := coreItem("versioned")
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}}); err != nil {
			t.Fatal(err)
		}
		// A stale extracted version of an older content hash does not make the
		// identity known once the entry's current content is feed-sourced again.
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO contents(entry_id,content_hash,sanitizer_version,source,html,text)
		 SELECT id,decode($3,'hex'),1,'extracted','<p>old</p>','old' FROM entries WHERE feed_id=$1 AND identity_hash=$2`, f.feed, mustCoreHash(t, item.IdentityHash), coreHash("stale")); err != nil {
			t.Fatal(err)
		}
		known, err := f.s.KnownIdentities(ctx, f.feed, []string{item.IdentityHash})
		if err != nil {
			t.Fatal(err)
		}
		if known[item.IdentityHash] {
			t.Error("stale extraction suppressed retry for changed content")
		}
	})
	t.Run("extracted_current_content_reused", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		item := coreItem("cached")
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}}); err != nil {
			t.Fatal(err)
		}
		extracted := item
		extracted.ContentSource = ContentFromExtracted
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{extracted}}); err != nil {
			t.Fatal(err)
		}
		known, err := f.s.KnownIdentities(ctx, f.feed, []string{item.IdentityHash})
		if err != nil || !known[item.IdentityHash] {
			t.Fatalf("cached extraction not reused: %v %v", known, err)
		}
	})
	t.Run("tombstone_without_extraction_known", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		item := coreItem("gone")
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=$2 WHERE feed_id=$1", f.feed, f.s.now()); err != nil {
			t.Fatal(err)
		}
		known, err := f.s.KnownIdentities(ctx, f.feed, []string{item.IdentityHash})
		if err != nil || !known[item.IdentityHash] {
			t.Fatalf("tombstone excluded from known set: %v %v", known, err)
		}
	})
	t.Run("initial_cohort_needs_extraction", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		items := []IngestItem{coreItem("a"), coreItem("b")}
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: items}); err != nil {
			t.Fatal(err)
		}
		known, err := f.s.KnownIdentities(ctx, f.feed, []string{items[0].IdentityHash, items[1].IdentityHash})
		if err != nil || len(known) != 0 {
			t.Fatalf("initial cohort suppressed extraction: %v %v", known, err)
		}
	})
	t.Run("argument_validation", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.s.KnownIdentities(ctx, 0, []string{coreHash("x")}); err == nil {
			t.Fatal("zero feed accepted")
		}
		if _, err := f.s.KnownIdentities(ctx, f.feed, []string{"not-hex"}); err == nil {
			t.Fatal("malformed hash accepted")
		}
	})
}
