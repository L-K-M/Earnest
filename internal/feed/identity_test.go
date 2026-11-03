package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func timePtr(t string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		panic(err)
	}
	return &parsed
}

func TestIdentityHashPrecedence(t *testing.T) {
	t.Parallel()

	withGUID := Item{GUID: "urn:uuid:1234", URL: "https://example.com/a", Title: "Same"}
	withoutGUID := Item{URL: "https://example.com/a", Title: "Same"}
	titleOnly := Item{Title: "Same"}

	if IdentityHash(withGUID) == IdentityHash(withoutGUID) {
		t.Fatal("guid must take precedence over the link")
	}
	if IdentityHash(withoutGUID) == IdentityHash(titleOnly) {
		t.Fatal("link must take precedence over title+published")
	}
}

func TestIdentityHashCanonicalisesURL(t *testing.T) {
	t.Parallel()

	plain := Item{URL: "https://example.com/a?keep=1"}
	tracked := Item{URL: "https://example.com/a?utm_source=news&keep=1#top"}
	if IdentityHash(plain) != IdentityHash(tracked) {
		t.Fatalf("tracking parameters changed the identity: %s vs %s", IdentityHash(plain), IdentityHash(tracked))
	}
}

func TestIdentityHashGUIDStability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		guid      string
		permalink *bool
		wantGUID  bool
	}{
		{"unspecified opaque", "item-123", nil, true},
		{"explicit permalink", "item-123", boolPtr(true), true},
		{"non-permalink opaque", "item-123", boolPtr(false), false},
		{"non-permalink urn", "urn:uuid:abc", boolPtr(false), true},
		{"non-permalink tag", "tag:example.com,2026:1", boolPtr(false), true},
		{"non-permalink uuid", "123e4567-e89b-12d3-a456-426614174000", boolPtr(false), true},
		{"non-permalink url", "https://example.com/p/1", boolPtr(false), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			it := Item{
				GUID:          tt.guid,
				URL:           "https://example.com/fallback",
				Title:         "Fallback",
				Published:     timePtr("2026-01-02T03:04:05Z"),
				guidPermalink: tt.permalink,
			}
			got := IdentityHash(it)

			var want string
			if tt.wantGUID {
				source := "guid:" + canonicalIdentityValue(tt.guid)
				sum := sha256.Sum256([]byte(source))
				want = hex.EncodeToString(sum[:])
			} else {
				sum := sha256.Sum256([]byte("url:" + CleanURL(it.URL)))
				want = hex.EncodeToString(sum[:])
			}
			if got != want {
				t.Fatalf("IdentityHash = %s, want %s", got, want)
			}
		})
	}
}

func TestIdentityHashTitleFallbackNormalisation(t *testing.T) {
	t.Parallel()

	base := Item{Title: "Hello world", Published: timePtr("2026-01-02T03:04:05Z")}
	whitespace := Item{Title: "  Hello \n\t world  ", Published: timePtr("2026-01-02T03:04:05Z")}
	different := Item{Title: "Hello there", Published: timePtr("2026-01-02T03:04:05Z")}

	if IdentityHash(base) != IdentityHash(whitespace) {
		t.Fatal("whitespace differences must not change the identity")
	}
	if IdentityHash(base) == IdentityHash(different) {
		t.Fatal("different titles must produce different identities")
	}
	if IdentityHash(base) == IdentityHash(Item{Title: base.Title}) {
		t.Fatal("the published date participates in the fallback identity")
	}
}

func TestIdentityHashEmptyItemIsDeterministic(t *testing.T) {
	t.Parallel()

	a := IdentityHash(Item{})
	b := IdentityHash(Item{})
	if a == "" || a != b {
		t.Fatalf("empty item hash = %q then %q, want equal non-empty values", a, b)
	}
}

func TestContentHashDetectsEdits(t *testing.T) {
	t.Parallel()

	base := Item{Title: "Story", ContentHTML: "<p>First paragraph</p><p>Second</p>"}

	textEdit := base
	textEdit.ContentHTML = "<p>First paragraph</p><p>Second edited</p>"
	if ContentHash(base) == ContentHash(textEdit) {
		t.Fatal("text edits must change the content hash")
	}

	markupOnly := base
	markupOnly.ContentHTML = `<p class="x">First paragraph</p><p>Second</p>`
	if ContentHash(base) != ContentHash(markupOnly) {
		t.Fatal("markup-only changes must not change the content hash")
	}

	whitespaceOnly := base
	whitespaceOnly.ContentHTML = "<p>First   paragraph</p>\n<p>Second</p>"
	if ContentHash(base) != ContentHash(whitespaceOnly) {
		t.Fatal("whitespace-only changes must not change the content hash")
	}

	titleEdit := base
	titleEdit.Title = "Story v2"
	if ContentHash(base) == ContentHash(titleEdit) {
		t.Fatal("title edits must change the content hash")
	}

	summaryFallback := Item{Title: "Story", SummaryHTML: "<p>Summary</p>"}
	if ContentHash(summaryFallback) == ContentHash(Item{Title: "Story"}) {
		t.Fatal("summary must be hashed when content is absent")
	}
}

func TestIdentityHashKnownAnswers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		it   Item
		want string
	}{
		{"guid", Item{GUID: "urn:uuid:test"}, "3bd087803d5a696395fc4623c8302ec531e1a0f2409b11512189de791f8ab0c2"},
		{"url after cleaning", Item{URL: "https://example.com/a?utm_source=x"}, "04eb124138012504ac9b90b7b3d7a6f87b05316bcdf951dada78199878214305"},
		{"title and published", Item{Title: "Hello world", Published: timePtr("2006-01-02T03:04:05Z")}, "4c29da90916f4074be4f157c567f21e8e79131699bcf54b6a669933ca8326882"},
		{"title without published", Item{Title: "Hello world"}, "a618f47038e6bc3f30a7dcfce81851326ecdf0dd7a7619550ed23f8905c2d34a"},
		{"empty item", Item{}, "bcd50e203968b43842d6194605bac556a826ac83fbf758ee19543a6019f29b63"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IdentityHash(tt.it); got != tt.want {
				t.Fatalf("IdentityHash = %s, want %s", got, tt.want)
			}
		})
	}

	content := Item{Title: "Story", ContentHTML: "<p>First paragraph</p><p>Second</p>"}
	if got := ContentHash(content); got != "8d0b624f726be1f94900107198429fd5ed6b61c34068cf09da08612d9483638f" {
		t.Fatalf("ContentHash = %s, want pinned digest", got)
	}
}
