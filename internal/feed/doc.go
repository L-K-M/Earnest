// Package feed parses RSS 0.9x/2.0, RSS 1.0, Atom 0.3/1.0 and JSON Feed
// 1.0/1.1 into Earnest's own types, derives entry identities and content
// hashes, canonicalises URLs, discovers feeds from HTML pages and reads and
// writes OPML.
//
// The package never performs network I/O: it parses bytes it is given. The
// fetcher supplies the HTTP charset and the URL the document was fetched
// from through ParseOptions.
//
// Security limits are part of every entry point. Documents carrying a DTD or
// entity declarations are rejected outright; Go's encoding/xml performs no
// entity expansion beyond the five predefined XML entities, so billion-laughs
// and external-entity attacks have nothing to expand. Input size, item count
// and parse time are bounded by Limits.
//
// # Entry identity rule
//
// IdentityHash is deliberately independent of the feed ID, because callers
// mix in their own feed scope. It returns SHA-256 over a canonical string
// built from the first available source:
//
//  1. "guid:" + canonical value, when GUID is non-empty and stable. A URL
//     GUID is canonicalised like a link but keeps its fragment, which comment
//     feeds use to tell comments on one post apart. A GUID is
//     stable when it is an absolute http(s) URL, when the source marked it as
//     a permalink (RSS isPermaLink defaults to true), or when it is a
//     URN-shaped identifier (urn:, tag:, UUID). An RSS GUID explicitly marked
//     isPermaLink="false" that has none of those shapes is treated as
//     unstable and skipped, because some publishers rotate such values.
//  2. "url:" + CleanURL(Item.URL), when the item has a canonicalisable link.
//  3. "title:" + normalised title + "\npublished:" + published time in RFC
//     3339 UTC (empty when absent).
//
// Each source carries its prefix so values from different sources cannot
// collide. Normalisation trims and collapses whitespace but does not change
// case. Changing this rule would store every affected article again, so it
// changes only with a migration path: GUID fragments joined the identity on
// 2026-11-09, and LegacyIdentityHash lets a fetch adopt an entry stored under
// the earlier rule instead of duplicating it.
package feed
