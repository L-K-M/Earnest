package service

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"time"
)

const (
	defaultCacheMaxInterval = 48 * time.Hour
	unknownFeedInterval     = time.Hour
	cadenceSampleSize       = 20
)

// FetchPolicy bounds ordinary polling separately from publisher cache hints.
// Error backoff and Retry-After are not limited by the healthy cache ceiling.
type FetchPolicy struct {
	MinInterval, MaxInterval, CacheMaxInterval time.Duration
}

func (p FetchPolicy) normalized() (FetchPolicy, error) {
	if p.MinInterval == 0 {
		p.MinInterval = minimumFetchInterval
	}
	if p.MaxInterval == 0 {
		p.MaxInterval = maximumFetchInterval
	}
	if p.CacheMaxInterval == 0 {
		p.CacheMaxInterval = defaultCacheMaxInterval
	}
	if p.MinInterval < 0 || p.MaxInterval < p.MinInterval || p.CacheMaxInterval < p.MinInterval {
		return FetchPolicy{}, fmt.Errorf("invalid feed scheduling intervals: %w", ErrInvalid)
	}
	return p, nil
}

type feedCadence struct {
	interval time.Duration
	latest   *time.Time
}

// observe uses the median gap between the latest distinct publication dates.
// The watermark prevents unchanged 200 responses from repeatedly weighting the
// same sample. A 304, failure, or undated feed leaves the estimate intact.
func (c feedCadence) observe(items []IngestItem, now time.Time) feedCadence {
	dates := make([]time.Time, 0, len(items)+1)
	for _, item := range items {
		if at := item.PublishedAt; at != nil && !at.IsZero() && !at.After(now) {
			dates = append(dates, at.UTC())
		}
	}
	if len(dates) == 0 {
		return c
	}
	slices.SortFunc(dates, func(a, b time.Time) int { return b.Compare(a) })
	latest := dates[0]
	if c.latest != nil && !latest.After(*c.latest) {
		return c
	}
	// Feeds that expose only their newest entry can still learn the gap
	// between successive publications, without treating fetch time as one.
	if c.latest != nil {
		dates = append(dates, *c.latest)
		slices.SortFunc(dates, func(a, b time.Time) int { return b.Compare(a) })
	}
	dates = slices.CompactFunc(dates, func(a, b time.Time) bool { return a.Equal(b) })
	dates = dates[:min(len(dates), cadenceSampleSize)]
	c.latest = &latest
	if len(dates) < 2 {
		return c
	}
	gaps := make([]time.Duration, 0, len(dates)-1)
	for i := 1; i < len(dates); i++ {
		gaps = append(gaps, dates[i-1].Sub(dates[i]))
	}
	slices.Sort(gaps)
	sample := gaps[len(gaps)/2]
	if len(gaps)%2 == 0 {
		lower := gaps[len(gaps)/2-1]
		sample = lower + (sample-lower)/2
	}
	// Sub-second and multi-year gaps cannot usefully refine our polling
	// bounds. Keep persisted intervals representable at PostgreSQL precision.
	sample = min(max(sample, time.Second), 365*24*time.Hour)
	if c.interval == 0 {
		c.interval = sample
	} else {
		// EWMA, 25% new sample. Subtraction avoids overflowing on extreme
		// publisher dates, unlike summing three copies of the old estimate.
		c.interval += (sample - c.interval) / 4
	}
	return c
}

func (p FetchPolicy) cacheCeiling(c feedCadence) time.Duration {
	if c.interval == 0 {
		return min(max(unknownFeedInterval, p.MinInterval), p.CacheMaxInterval)
	}
	// Clamp before doubling so even hostile dates cannot overflow Duration.
	if c.interval >= p.CacheMaxInterval/2 {
		return p.CacheMaxInterval
	}
	return min(max(2*c.interval, p.MinInterval), p.CacheMaxInterval)
}

func (p FetchPolicy) healthySchedule(c feedCadence, cacheAge time.Duration) (interval, delay time.Duration) {
	interval = c.interval
	if interval == 0 {
		interval = unknownFeedInterval
	}
	// Jitter only ordinary polling, never the bounded cache hint. Clamp it
	// to the same bounds so a configured minimum or maximum stays meaningful.
	delay = min(max(jitterInterval(interval), p.MinInterval), p.MaxInterval)
	interval = min(max(interval, p.MinInterval), p.MaxInterval)
	delay = min(delay, max(interval, p.cacheCeiling(c)))
	return interval, max(delay, min(cacheAge, p.cacheCeiling(c)))
}

func jitterInterval(interval time.Duration) time.Duration {
	// Use subtraction for the upper half to avoid overflowing long durations.
	spread := interval / 10
	if rand.IntN(2) == 0 {
		return interval - time.Duration(rand.Float64()*float64(spread))
	}
	const maxDuration = time.Duration(1<<63 - 1)
	return interval + time.Duration(rand.Float64()*float64(min(spread, maxDuration-interval)))
}

func (p FetchPolicy) failureSchedule(o FetchOutcome, interval time.Duration, failures int) (time.Duration, time.Duration) {
	interval = min(max(interval, p.MinInterval), p.MaxInterval)
	retry := p.MaxInterval
	permanent := o.ErrorClass == "gone" || o.ErrorClass == "challenge" || o.ErrorClass == "policy" ||
		(o.ErrorClass == "http" && o.HTTPStatus == 0) || // redirect loop, with no final response
		(o.HTTPStatus >= 400 && o.HTTPStatus < 500 && o.HTTPStatus != 429)
	if !permanent {
		jittered := jitterInterval(interval)
		if jittered < p.MaxInterval/2 {
			retry = max(2*jittered, p.MinInterval)
		}
	}
	backoff := fetchBackoff(o.ErrorClass, failures, o.RetryAfter)
	// Cache-Control on an error page must not influence recovery. Preserve
	// even a long explicit Retry-After independently of the cache ceiling.
	return interval, max(retry, backoff, o.RetryAfter)
}
