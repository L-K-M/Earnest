package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IdentityHash returns the SHA-256 hex digest that identifies an entry
// independent of its feed. See the package documentation for the exact rule;
// it is frozen because clients cache it.
func IdentityHash(it Item) string {
	var source string
	switch {
	case stableGUID(it.GUID, it.guidPermalink):
		source = "guid:" + canonicalIdentityValue(it.GUID)
	default:
		if u := CleanURL(it.URL); u != "" {
			source = "url:" + u
		} else {
			source = "title:" + normalizeTitle(it.Title) + "\npublished:" + publishedKey(it.Published)
		}
	}
	return hashHex(source)
}

// ContentHash returns the SHA-256 hex digest over the normalised title and
// the text content of the entry, detecting edits without creating duplicates.
// Markup changes that do not change the visible text do not change the hash.
func ContentHash(it Item) string {
	content := it.ContentHTML
	if strings.TrimSpace(content) == "" {
		content = it.SummaryHTML
	}
	source := "title:" + normalizeTitle(it.Title) + "\ncontent:" + htmlText(content)
	return hashHex(source)
}

// stableGUID reports whether a GUID may serve as an identity component. A
// GUID explicitly marked isPermaLink="false" is only trusted when it has a
// shape that is stable by construction: an absolute http(s) URL, a URN or tag
// URI, or a UUID. Anything else could be rotated by the publisher, and using a
// rotating value would create a duplicate entry every time it changed.
func stableGUID(guid string, permalink *bool) bool {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return false
	}
	if permalink != nil && !*permalink {
		return isStableIdentifier(guid)
	}
	return true
}

func isStableIdentifier(s string) bool {
	if CleanURL(s) != "" {
		return true
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "urn:") || strings.HasPrefix(lower, "tag:") {
		return len(s) > 4
	}
	return uuidRE.MatchString(s)
}

// canonicalIdentityValue cleans URL-shaped values and leaves opaque
// identifiers as written. A URL keeps its fragment: in a link the fragment
// only scrolls the page, but in a GUID it is part of the identifier, and
// comment feeds use it to tell the comments on one post apart
// ("https://example.com/?p=1#comment-7").
func canonicalIdentityValue(s string) string {
	cleaned := CleanURL(s)
	if cleaned == "" {
		return strings.TrimSpace(s)
	}
	if u, err := url.Parse(strings.TrimSpace(s)); err == nil && u.Fragment != "" {
		return cleaned + "#" + u.EscapedFragment()
	}
	return cleaned
}

// LegacyIdentityHash returns the identity an entry was stored under before
// GUID fragments became part of the identity (2026-11-09), or "" when both
// rules agree. A fetch uses it to adopt such an entry instead of storing the
// article again under its new identity.
func LegacyIdentityHash(it Item) string {
	if !stableGUID(it.GUID, it.guidPermalink) {
		return ""
	}
	cleaned := CleanURL(it.GUID)
	if cleaned == "" {
		return ""
	}
	legacy := hashHex("guid:" + cleaned)
	if legacy == IdentityHash(it) {
		return ""
	}
	return legacy
}

func normalizeTitle(s string) string {
	return collapseWS(s)
}

func publishedKey(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
