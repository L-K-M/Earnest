package service

import "time"

// The fetcher claims due feeds and reports what it fetched through
// these types; the service owns persistence, seq and change events.
//
//	fetcher ── ClaimDueFeeds ──► service ──► store
//	   │  fetch (outbound), parse (feed), sanitise, extract
//	   └────── RecordFetch(FetchOutcome) ──► service: entries, seq, changes, health

// ExtractMode says whether entries get full-text extraction.
type ExtractMode string

const (
	ExtractNone   ExtractMode = "none"
	ExtractAlways ExtractMode = "always"
)

// FetchStatus is the outcome of one fetch attempt.
type FetchStatus string

const (
	FetchOK          FetchStatus = "ok"
	FetchNotModified FetchStatus = "not_modified"
	FetchFailed      FetchStatus = "failed"
)

// ContentSource says where stored content came from.
type ContentSource string

const (
	ContentFromFeed      ContentSource = "feed"
	ContentFromExtracted ContentSource = "extracted"
)

// FeedCredentials are decrypted only for the fetch and never logged.
type FeedCredentials struct {
	Username string
	Password string
	Headers  map[string]string
}

// FeedToFetch is a feed the fetcher has claimed until LeaseUntil.
type FeedToFetch struct {
	FeedID       int64
	URL          string
	ETag         string
	LastModified string
	Credentials  *FeedCredentials // nil for public feeds
	Extract      ExtractMode
	Interval     time.Duration // current adaptive interval
	LeaseUntil   time.Time
}

// FeedMeta is what the feed says about itself.
type FeedMeta struct {
	Title    string
	SiteURL  string
	IconURL  string
	Language string
}

// Enclosure is a podcast or media attachment, shown with a link.
type Enclosure struct {
	URL    string
	Type   string
	Length int64
}

// IngestItem is one parsed, sanitised entry.
type IngestItem struct {
	IdentityHash     string // feed.IdentityHash; unique per feed
	GUID             string
	URL              string
	Title            string
	Author           string
	PublishedAt      *time.Time // display only
	UpdatedAt        *time.Time
	ContentHash      string
	Language         string
	ContentHTML      string // sanitised
	ContentText      string
	SanitizerVersion int
	ContentSource    ContentSource
	Enclosures       []Enclosure
}

// FetchOutcome reports one attempt. On FetchFailed, Items is empty and
// ErrorClass is an outbound or parse class shown in feed health.
type FetchOutcome struct {
	FeedID               int64
	AttemptedAt          time.Time
	Status               FetchStatus
	HTTPStatus           int
	ErrorClass           string
	ErrorMessage         string
	RetryAfter           time.Duration
	ETag                 string
	LastModified         string
	PermanentRedirectURL string
	Feed                 *FeedMeta
	Items                []IngestItem
	NextFetchAt          time.Time
}

// IngestSummary says what RecordFetch changed.
type IngestSummary struct {
	Created int
	Updated int
}
