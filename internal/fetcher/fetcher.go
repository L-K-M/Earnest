// Package fetcher schedules and processes feed retrieval through outbound.
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/L-K-M/Earnest/internal/extract"
	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/metrics"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/sanitize"
	"github.com/L-K-M/Earnest/internal/service"
)

// Service is the persistence boundary used by the fetcher.
type Service interface {
	ClaimDueFeeds(context.Context, time.Time, int, time.Duration) ([]service.FeedToFetch, error)
	KnownIdentities(context.Context, int64, []string) (map[string]bool, error)
	RecordFetch(context.Context, service.FetchOutcome) (service.IngestSummary, error)
}

// Config controls scheduling and outbound limits.
type Config struct {
	MinInterval, MaxInterval time.Duration
	PollInterval, Lease      time.Duration
	Workers, BatchSize       int
	ExtraPorts               []uint16
	Metrics                  *metrics.Registry
}

const (
	// maxExtractsPerFetch bounds full-text extraction within one pass. Each
	// attempt is a bounded outbound GET, but a fresh subscription has one per
	// item; without a cap the item loop can outlive the claim lease and a
	// second worker claims the same feed. Skipped items stay unknown to
	// KnownIdentities and are extracted on later passes.
	maxExtractsPerFetch = 12
	// leaseMargin keeps the item loop clear of the claim lease deadline.
	leaseMargin = 30 * time.Second
)

// Fetcher coordinates retrieval and reporting.
type Fetcher struct {
	service Service
	client  *outbound.Client
	config  Config
	kick    chan struct{}
}

// New constructs a fetcher with sensible defaults.
func New(s Service, c *outbound.Client, cfg Config) (*Fetcher, error) {
	if s == nil || c == nil {
		return nil, errors.New("fetcher: service and client are required")
	}
	if cfg.MinInterval == 0 {
		cfg.MinInterval = 15 * time.Minute
	}
	if cfg.MaxInterval == 0 {
		cfg.MaxInterval = 24 * time.Hour
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Minute
	}
	if cfg.Lease == 0 {
		cfg.Lease = 10 * time.Minute
	}
	if cfg.Workers == 0 {
		cfg.Workers = 4
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = cfg.Workers
	}
	if cfg.MinInterval < 0 || cfg.MaxInterval < cfg.MinInterval || cfg.PollInterval < 0 || cfg.Lease < 0 || cfg.Workers < 1 || cfg.BatchSize < 1 {
		return nil, errors.New("fetcher: invalid config")
	}
	cfg.ExtraPorts = append([]uint16(nil), cfg.ExtraPorts...)
	return &Fetcher{service: s, client: c, config: cfg, kick: make(chan struct{}, 1)}, nil
}

// Kick wakes the scheduler after a subscription changes.
func (f *Fetcher) Kick() {
	select {
	case f.kick <- struct{}{}:
	default:
	}
}

// Run processes claimed feeds until cancellation or a claim error.
func (f *Fetcher) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if f.config.Metrics != nil {
		defer f.config.Metrics.SetQueueDepth(0)
	}
	errs := make(chan error, 1)
	workerError := func() error {
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	}
	jobs := make(chan service.FeedToFetch)
	var wg sync.WaitGroup
	for i := 0; i < f.config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if err := f.Fetch(ctx, item); err != nil && ctx.Err() == nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}
	defer func() { close(jobs); wg.Wait() }()
	for {
		feeds, err := f.service.ClaimDueFeeds(ctx, time.Now().UTC(), f.config.BatchSize, f.config.Lease)
		if err != nil {
			if ctx.Err() != nil {
				return workerError()
			}
			return fmt.Errorf("claim feeds: %w", err)
		}
		for i, item := range feeds {
			if f.config.Metrics != nil {
				f.config.Metrics.SetQueueDepth(len(feeds) - i)
			}
			select {
			case jobs <- item:
			case <-ctx.Done():
				return workerError()
			}
		}
		if f.config.Metrics != nil {
			f.config.Metrics.SetQueueDepth(0)
		}
		if len(feeds) == f.config.BatchSize {
			continue
		}
		timer := time.NewTimer(f.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return workerError()
		case <-f.kick:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// Fetcher-internal classes for failures outbound never produces.
const (
	classParse   outbound.Class = "parse"
	classHTTP4xx outbound.Class = "http_4xx"
)

// persistedErrorClass translates outbound (and fetcher-internal) failure
// classes into the vocabulary the service validates and persists as feed
// health. Scheduling keeps the outbound class; this mapping only crosses the
// service boundary.
func persistedErrorClass(class outbound.Class) string {
	switch class {
	case outbound.DNS:
		return "dns"
	case outbound.Connect:
		return "network"
	case outbound.TLS:
		return "tls"
	case outbound.Timeout:
		return "timeout"
	case outbound.DeniedAddress:
		return "policy"
	case outbound.TooLarge:
		return "size"
	case outbound.HTTPGone:
		return "gone"
	case outbound.HTTP429:
		return "rate_limit"
	case outbound.HTTP403:
		return "challenge"
	case outbound.HTTP5xx, outbound.RedirectLoop, classHTTP4xx:
		return "http"
	case outbound.Decode, classParse:
		return "parse"
	}
	return "parse"
}

func credentials(c *service.FeedCredentials) outbound.Credentials {
	var result outbound.Credentials
	if c != nil {
		result.Username = c.Username
		result.Password = c.Password
		result.Headers = make(http.Header)
		for k, v := range c.Headers {
			result.Headers.Set(k, v)
		}
	}
	return result
}

// Fetch retrieves one claimed feed and records its outcome.
func (f *Fetcher) Fetch(ctx context.Context, item service.FeedToFetch) error {
	now := time.Now().UTC()
	out := service.FetchOutcome{FeedID: item.FeedID, AttemptedAt: now, Status: service.FetchFailed}
	target := item.FetchURL
	if target == "" {
		target = item.URL
	}
	// Credentials belong to the feed's own origin: a recorded permanent
	// redirect must not send them to another host (mirrors Probe scoping).
	var scoped *service.FeedCredentials
	if sameOrigin(item.URL, target) {
		scoped = item.Credentials
	}
	result, err := f.client.Get(ctx, target, outbound.Policy{Purpose: outbound.Feed, Credentials: credentials(scoped), ETag: item.ETag, LastModified: item.LastModified, ExtraPorts: f.config.ExtraPorts})
	var parsed *feed.Feed
	if result != nil {
		resp := result.Response
		defer resp.Body.Close()
		out.HTTPStatus = resp.StatusCode
		if result.PermanentRedirect {
			// Fragments are not sent in HTTP requests and cannot be stored
			// as feed URLs. Keep both normal and failure outcomes valid.
			redirect := *result.FinalURL
			redirect.Fragment, redirect.RawFragment = "", ""
			out.PermanentRedirectURL = redirect.String()
		}
		out.ETag = resp.Header.Get("ETag")
		out.LastModified = resp.Header.Get("Last-Modified")
		if resp.StatusCode == http.StatusNotModified {
			if out.ETag == "" {
				out.ETag = item.ETag
			}
			if out.LastModified == "" {
				out.LastModified = item.LastModified
			}
		}
		if err == nil && resp.StatusCode == http.StatusNotModified {
			out.Status = service.FetchNotModified
		} else if err == nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
			err = fmt.Errorf("HTTP status %d", resp.StatusCode)
		} else if err == nil {
			var data []byte
			data, err = io.ReadAll(resp.Body)
			if err == nil {
				parsed, err = feed.Parse(data, feed.ParseOptions{BaseURL: result.FinalURL.String(), Charset: resp.Header.Get("Content-Type")})
				if err != nil {
					err = fmt.Errorf("parse feed: %w", err)
				}
			}
			if err == nil {
				out.Feed = metadata(parsed)
				out.Items, err = f.items(ctx, item, parsed.Items)
				if err == nil {
					out.Status = service.FetchOK
				}
			}
		}
	}
	var class outbound.Class
	if err != nil {
		class = outbound.ErrorClass(err)
		if class == "" {
			if out.HTTPStatus >= 400 && out.HTTPStatus < 500 {
				class = classHTTP4xx
			} else {
				class = classParse
			}
		}
		out.ErrorClass = persistedErrorClass(class)
		out.ErrorMessage = err.Error()
		out.Items = nil
		var oe *outbound.Error
		if errors.As(err, &oe) && (out.HTTPStatus == 429 || out.HTTPStatus == 503) {
			out.RetryAfter = oe.RetryAfter
		}
	}
	interval := f.interval(item, parsed, out.Status, class)
	if result != nil {
		if age, ok := outbound.ParseMaxAge(result.Response.Header.Get("Cache-Control")); ok && age > interval {
			interval = age
		}
	}
	if out.RetryAfter > interval {
		interval = out.RetryAfter
	}
	out.NextFetchAt = now.Add(interval)
	summary, recordErr := f.service.RecordFetch(ctx, out)
	if recordErr != nil && out.Status != service.FetchFailed && errors.Is(recordErr, service.ErrInvalid) {
		// The service refused what this feed served. That is one feed's
		// problem, not the server's: record it as a parse failure, so feed
		// health shows it and the schedule moves on, and keep fetching the
		// other feeds. The cache validators are dropped so the next fetch
		// reads the whole document again.
		slog.Warn("feed data rejected", "feed_id", out.FeedID, "error", recordErr)
		failed := service.FetchOutcome{
			FeedID: out.FeedID, AttemptedAt: out.AttemptedAt, Status: service.FetchFailed, HTTPStatus: out.HTTPStatus,
			ErrorClass: persistedErrorClass(classParse), ErrorMessage: "the feed's items were rejected: " + recordErr.Error(),
			PermanentRedirectURL: out.PermanentRedirectURL, NextFetchAt: out.NextFetchAt,
		}
		out = failed
		summary, recordErr = f.service.RecordFetch(ctx, failed)
	}
	if f.config.Metrics != nil {
		class := string(out.Status)
		if out.ErrorClass != "" {
			class = out.ErrorClass
		}
		f.config.Metrics.ObserveFetch(class, time.Since(now).Seconds())
		if recordErr == nil {
			f.config.Metrics.AddItems(summary.Created + summary.Updated)
		}
	}
	if recordErr != nil {
		return fmt.Errorf("record fetch: %w", recordErr)
	}
	return nil
}

func (f *Fetcher) interval(item service.FeedToFetch, parsed *feed.Feed, status service.FetchStatus, class outbound.Class) time.Duration {
	d := time.Hour
	if status == service.FetchFailed {
		switch class {
		case outbound.HTTPGone, outbound.HTTP403, outbound.DeniedAddress, outbound.RedirectLoop, classHTTP4xx:
			return f.config.MaxInterval
		}
		d = item.Interval * 2
		if d <= 0 {
			d = time.Hour
		}
	} else if parsed != nil {
		dates := make([]time.Time, 0, len(parsed.Items))
		for _, it := range parsed.Items {
			if it.Published != nil && !it.Published.After(time.Now()) {
				dates = append(dates, *it.Published)
			}
		}
		sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
		if len(dates) > 1 {
			d = dates[len(dates)-1].Sub(dates[0]) / time.Duration(len(dates)-1)
		}
	}
	if status != service.FetchFailed && d >= time.Hour {
		d = time.Duration(float64(d) * (1 + rand.Float64()*0.1))
	} else {
		d = time.Duration(float64(d) * (0.9 + rand.Float64()*0.2))
	}
	if d < f.config.MinInterval {
		d = f.config.MinInterval
	}
	if d > f.config.MaxInterval {
		d = f.config.MaxInterval
	}
	return d
}
func metadata(p *feed.Feed) *service.FeedMeta {
	return &service.FeedMeta{Title: p.Title, SiteURL: p.SiteURL, IconURL: p.IconURL, Language: p.Language}
}

// itemBase returns the item link as a base URL for resolving relative content
// URLs. Item links are publisher-controlled and can carry active schemes
// (javascript:, data:), so only absolute http(s) links qualify; everything
// else yields nil and relative content URLs are dropped.
func itemBase(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	return u
}

func (f *Fetcher) items(ctx context.Context, claim service.FeedToFetch, list []feed.Item) ([]service.IngestItem, error) {
	known := map[string]bool{}
	if claim.Extract == service.ExtractAlways && len(list) > 0 {
		hashes := make([]string, len(list))
		for i, it := range list {
			hashes[i] = feed.IdentityHash(it)
		}
		var err error
		known, err = f.service.KnownIdentities(ctx, claim.FeedID, hashes)
		if err != nil {
			return nil, fmt.Errorf("known identities: %w", err)
		}
	}
	result := make([]service.IngestItem, 0, len(list))
	// Extraction work stops at the smaller of a count budget and the claim
	// lease deadline, so one pass can never run past its lease.
	extractDeadline := time.Time{}
	if !claim.LeaseUntil.IsZero() {
		extractDeadline = claim.LeaseUntil.Add(-leaseMargin)
	}
	var jobs []extractJob
	// Publishers repeat items, and distinct items can share an identity. The
	// service accepts each identity once per fetch, so the first wins.
	seen := make(map[string]bool, len(list))
	for _, it := range list {
		identity := feed.IdentityHash(it)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		base := itemBase(it.URL)
		link := ""
		if base != nil {
			// Only absolute http(s) links reach a client that renders them as
			// anchors; a stored active-scheme link is a stored attack.
			link = strings.TrimSpace(it.URL)
		} else if ref, err := url.Parse(strings.TrimSpace(it.URL)); err == nil && ref.Scheme == "" && (ref.Host != "" || ref.Path != "") {
			// Relative item links are valid RSS/Atom. The parser resolves them
			// against the feed base, but a feed with a broken base URI can still
			// hand one over; resolve it against the fetch URL so the item keeps
			// a usable link. Active-scheme values have a scheme and stay dropped;
			// a blank, fragment-only or query-only reference names no resource
			// and would otherwise resolve to the feed URL itself.
			// Resolve against the URL actually fetched: after a permanent
			// redirect that is FetchURL, not the canonical feed URL.
			fetched := claim.FetchURL
			if fetched == "" {
				fetched = claim.URL
			}
			if feedBase := itemBase(fetched); feedBase != nil {
				base = itemBase(feedBase.ResolveReference(ref).String())
				if base != nil {
					link = base.String()
				}
			}
		}
		content := it.ContentHTML
		if content == "" {
			content = it.SummaryHTML
		}
		doc, err := sanitize.Sanitize(base, content)
		if err != nil {
			// One unsalvageable item must not silence the whole feed: keep the
			// item without a body rather than failing the fetch. ErrUnstable
			// callers must store nothing instead of the raw input.
			doc = sanitize.Document{Version: sanitize.Version}
		}
		source := service.ContentFromFeed
		if it.ContentHTML == "" {
			source = service.ContentFromFeedExcerpt
		}
		if claim.Extract == service.ExtractAlways && base != nil && !known[identity] &&
			len(jobs) < maxExtractsPerFetch && (extractDeadline.IsZero() || time.Now().Before(extractDeadline)) {
			// The index is the item's place in result, which skipped
			// repeats, not in list.
			jobs = append(jobs, extractJob{index: len(result), link: link})
		}
		entry := service.IngestItem{IdentityHash: identity, LegacyIdentityHash: feed.LegacyIdentityHash(it), GUID: it.GUID, URL: link, Title: it.Title, Author: it.Author, PublishedAt: it.Published, UpdatedAt: it.Updated, ContentHash: feed.ContentHash(it), Language: it.Language, ContentHTML: doc.HTML, ContentText: sanitize.Text(doc.HTML), SanitizerVersion: int(doc.Version), ContentSource: source, Image: it.Image}
		for _, enc := range it.Enclosures {
			entry.Enclosures = append(entry.Enclosures, service.Enclosure{URL: enc.URL, Type: enc.Type, Length: enc.Length})
		}
		result = append(result, entry)
	}
	// Results land back on items by index, so overlapping fetches cannot
	// reorder the list.
	for i, extracted := range f.extractItems(ctx, jobs, extractDeadline) {
		result[i].ContentHTML = extracted.document.HTML
		result[i].ContentText = sanitize.Text(extracted.document.HTML)
		result[i].SanitizerVersion = int(extracted.document.Version)
		result[i].ContentSource = extracted.source
	}
	return result, nil
}

// extractWorkers bounds concurrent full-text extraction. The outbound client
// still spaces requests per host, so this mainly overlaps different hosts and
// hides readability CPU time instead of serialising the whole item list.
const extractWorkers = 4

// extractJob is one full-text extraction selected in items(): the item index
// the result applies to and the resolved http(s) link to fetch.
type extractJob struct {
	index int
	link  string
}

type extractionResult struct {
	document sanitize.Document
	source   service.ContentSource
}

// extractItems runs the selected jobs through a pool of extractWorkers. A
// worker starts no new fetch once ctx is done or the lease deadline has
// passed; an in-flight GET still ends with ctx. Failures leave no result, so
// the item keeps its feed body: an extraction error is not a feed error.
func (f *Fetcher) extractItems(ctx context.Context, jobs []extractJob, deadline time.Time) map[int]extractionResult {
	if len(jobs) == 0 {
		return nil
	}
	results := make(map[int]extractionResult, len(jobs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	// Buffered so the producer never blocks if every worker stops early on a
	// cancelled context or an expired lease.
	jobsCh := make(chan extractJob, len(jobs))
	workers := min(extractWorkers, len(jobs))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				if ctx.Err() != nil || (!deadline.IsZero() && !time.Now().Before(deadline)) {
					return
				}
				r, e := f.client.Get(ctx, j.link, outbound.Policy{Purpose: outbound.Extract, ExtraPorts: f.config.ExtraPorts})
				if e != nil || r == nil {
					if r != nil {
						r.Response.Body.Close()
					}
					continue
				}
				body, readErr := io.ReadAll(r.Response.Body)
				r.Response.Body.Close()
				if readErr != nil || r.Response.StatusCode < 200 || r.Response.StatusCode >= 300 {
					continue
				}
				extracted, extractErr := extract.Extract(r.FinalURL, body)
				if extractErr != nil || extracted.Status != extract.StatusOK {
					continue
				}
				mu.Lock()
				results[j.index] = extractionResult{document: extracted.Document, source: service.ContentFromExtracted}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		jobsCh <- j
	}
	close(jobsCh)
	wg.Wait()
	return results
}

// ProbeError classifies a discovery failure.
type ProbeError struct {
	Class string
	Err   error
}

func (e *ProbeError) Error() string { return e.Class + ": " + e.Err.Error() }
func (e *ProbeError) Unwrap() error { return e.Err }

// parseOrigin returns the lowercase scheme://host:port of an absolute URL.
// Default ports are made explicit so https://host and https://host:443 are
// one origin; relative or malformed URLs report ok=false and compare unequal
// to anything, so scoping fails closed.
func parseOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname()) + ":" + port, true
}

func sameOrigin(a, b string) bool {
	left, ok := parseOrigin(a)
	if !ok {
		return false
	}
	right, ok := parseOrigin(b)
	return ok && left == right
}

// Probe discovers a feed and returns its parsed, sanitised contents.
func (f *Fetcher) Probe(ctx context.Context, rawURL string, creds *service.FeedCredentials) (string, service.FeedMeta, []service.IngestItem, error) {
	urls := []string{rawURL}
	original, _ := url.Parse(rawURL)
	seen := map[string]bool{}
	var last error
	for len(urls) > 0 && len(seen) < 32 {
		target := urls[0]
		urls = urls[1:]
		if seen[target] {
			continue
		}
		seen[target] = true
		targetURL, _ := url.Parse(target)
		var scoped *service.FeedCredentials
		if original != nil && targetURL != nil && strings.EqualFold(original.Scheme, targetURL.Scheme) && strings.EqualFold(original.Host, targetURL.Host) {
			scoped = creds
		}
		r, err := f.client.Get(ctx, target, outbound.Policy{Purpose: outbound.Discovery, Credentials: credentials(scoped), ExtraPorts: f.config.ExtraPorts})
		if err != nil {
			if r != nil {
				r.Response.Body.Close()
			}
			last = err
			continue
		}
		data, err := io.ReadAll(r.Response.Body)
		r.Response.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if r.Response.StatusCode < 200 || r.Response.StatusCode >= 300 {
			last = fmt.Errorf("HTTP status %d", r.Response.StatusCode)
			continue
		}
		parsed, err := feed.Parse(data, feed.ParseOptions{BaseURL: r.FinalURL.String(), Charset: r.Response.Header.Get("Content-Type")})
		if err == nil {
			items, e := f.items(ctx, service.FeedToFetch{}, parsed.Items)
			if e != nil {
				return "", service.FeedMeta{}, nil, &ProbeError{Class: "parse", Err: e}
			}
			return r.FinalURL.String(), *metadata(parsed), items, nil
		}
		last = err
		if strings.Contains(strings.ToLower(r.Response.Header.Get("Content-Type")), "html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(data))), "<!doctype html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(data))), "<html") {
			candidates, e := feed.Discover(data, r.FinalURL.String())
			if e == nil {
				for _, c := range candidates {
					if !seen[c.URL] {
						urls = append(urls, c.URL)
					}
				}
			}
		}
	}
	if last == nil {
		last = errors.New("no feed found")
	}
	class := string(outbound.ErrorClass(last))
	if class == "" {
		class = "parse"
	}
	return "", service.FeedMeta{}, nil, &ProbeError{Class: class, Err: last}
}
