package service

import (
	"testing"
	"time"
)

func scheduleItems(latest time.Time, gap time.Duration, count int) []IngestItem {
	items := make([]IngestItem, count)
	for i := range items {
		at := latest.Add(-time.Duration(i) * gap)
		items[i].PublishedAt = &at
	}
	return items
}

func TestPostingCadenceSamplesAndSmooths(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	items := scheduleItems(now, time.Hour, 20)
	future, zero, ancient := now.Add(time.Hour), time.Time{}, now.Add(-365*24*time.Hour)
	items = append(items, IngestItem{}, IngestItem{PublishedAt: &zero}, IngestItem{PublishedAt: &future}, items[0], IngestItem{PublishedAt: &ancient})
	c := (feedCadence{}).observe(items, now)
	if c.interval != time.Hour || c.latest == nil || !c.latest.Equal(now) {
		t.Fatalf("cadence = %+v, want hourly without invalid dates or old outlier", c)
	}
	if got := c.observe(nil, now); got.interval != c.interval || !got.latest.Equal(*c.latest) {
		t.Fatalf("missing dates lost cadence: %+v", got)
	}
	// Same newest timestamp, but a changed older sample. Re-fetching this
	// document must not repeatedly smooth the estimate toward its old burst.
	burst := scheduleItems(now, 10*time.Minute, 20)
	if got := c.observe(burst, now); got.interval != time.Hour {
		t.Fatalf("unchanged sample changed cadence: %v", got.interval)
	}
	later := now.Add(time.Hour)
	c = c.observe(scheduleItems(later, 10*time.Minute, 20), later)
	if c.interval != 47*time.Minute+30*time.Second {
		t.Fatalf("burst was not smoothed: %v", c.interval)
	}
	if got := c.observe(scheduleItems(later, 10*time.Minute, 20), later); got.interval != c.interval {
		t.Fatalf("replayed sample reweighted cadence: %v", got.interval)
	}
}

func TestPostingCadenceSparseAndExtremeDates(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c := (feedCadence{}).observe(scheduleItems(now, 0, 3), now)
	if c.interval != 0 || c.latest == nil {
		t.Fatalf("identical dates must not invent a rate: %+v", c)
	}
	later := now.Add(30 * 24 * time.Hour)
	c = c.observe(scheduleItems(later, 0, 1), later)
	if c.interval != 30*24*time.Hour {
		t.Fatalf("single-entry feed failed to learn publication gap: %v", c.interval)
	}
	for _, tc := range []struct {
		gap, want time.Duration
	}{
		{time.Nanosecond, time.Second},
		{time.Duration(1<<63 - 1), 365 * 24 * time.Hour},
	} {
		got := (feedCadence{}).observe(scheduleItems(now, tc.gap, 2), now)
		if got.interval != tc.want {
			t.Fatalf("gap %v stored as %v, want %v", tc.gap, got.interval, tc.want)
		}
	}
}

func TestHealthyFeedSchedule(t *testing.T) {
	p, err := (FetchPolicy{}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                  string
		cadence, age, ceiling time.Duration
	}{
		{"frequent", 10 * time.Minute, 48 * time.Hour, 20 * time.Minute},
		{"hourly", time.Hour, 48 * time.Hour, 2 * time.Hour},
		{"monthly", 30 * 24 * time.Hour, 7 * 24 * time.Hour, 48 * time.Hour},
		{"unknown", 0, 48 * time.Hour, time.Hour},
		{"sub_minimum", time.Second, 48 * time.Hour, 15 * time.Minute},
		{"extreme_header", time.Hour, time.Duration(1<<63 - 1), 2 * time.Hour},
		{"extreme_cadence", time.Duration(1<<63 - 1), 7 * 24 * time.Hour, 48 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 100 {
				interval, delay := p.healthySchedule(feedCadence{interval: tc.cadence}, tc.age)
				if delay != tc.ceiling {
					t.Fatalf("delay %v, want %v", delay, tc.ceiling)
				}
				if interval < p.MinInterval || interval > p.MaxInterval {
					t.Fatalf("normal interval outside configured bounds: %v", interval)
				}
			}
		})
	}
	// The cache ceiling is not itself the polling interval.
	for range 100 {
		_, delay := p.healthySchedule(feedCadence{interval: time.Hour}, 0)
		if delay < 54*time.Minute || delay > 66*time.Minute {
			t.Fatalf("ordinary hourly polling became %v", delay)
		}
		_, delay = p.healthySchedule(feedCadence{interval: 7 * 24 * time.Hour}, 0)
		if delay != p.MaxInterval {
			t.Fatalf("weekly feed should use normal maximum: %v", delay)
		}
	}
}

func TestFetchPolicyCustomBounds(t *testing.T) {
	for _, p := range []FetchPolicy{
		{MinInterval: -time.Minute},
		{MaxInterval: time.Minute},
		{CacheMaxInterval: time.Minute},
	} {
		if _, err := p.normalized(); err == nil {
			t.Fatalf("accepted invalid policy: %+v", p)
		}
	}
	p, err := (FetchPolicy{MinInterval: 5 * time.Minute, MaxInterval: 12 * time.Hour, CacheMaxInterval: 24 * time.Hour}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if _, delay := p.healthySchedule(feedCadence{interval: 30 * 24 * time.Hour}, 7*24*time.Hour); delay != 24*time.Hour {
		t.Fatalf("custom cache maximum ignored: %v", delay)
	}
}

func TestFailureScheduleIsIndependentOfHealthyCacheCap(t *testing.T) {
	p, _ := (FetchPolicy{}).normalized()
	for range 100 {
		_, delay := p.failureSchedule(FetchOutcome{ErrorClass: "network"}, time.Hour, 0)
		if delay < 108*time.Minute || delay > 132*time.Minute {
			t.Fatalf("failure should retry around twice the polling interval: %v", delay)
		}
		_, delay = p.failureSchedule(FetchOutcome{ErrorClass: "network"}, 48*time.Hour, 0)
		if delay != p.MaxInterval {
			t.Fatalf("slow failed feed should use normal maximum: %v", delay)
		}
	}
	for _, tc := range []struct {
		out  FetchOutcome
		want time.Duration
	}{
		{FetchOutcome{ErrorClass: "gone", HTTPStatus: 410}, 7 * 24 * time.Hour},
		{FetchOutcome{ErrorClass: "challenge", HTTPStatus: 403}, 24 * time.Hour},
		{FetchOutcome{ErrorClass: "http"}, 24 * time.Hour},
		{FetchOutcome{ErrorClass: "rate_limit", HTTPStatus: 429, RetryAfter: 10 * 24 * time.Hour}, 10 * 24 * time.Hour},
	} {
		tc.out.CacheMaxAge = 30 * 24 * time.Hour
		_, got := p.failureSchedule(tc.out, time.Hour, 0)
		if got != tc.want {
			t.Fatalf("%s: retry %v, want %v", tc.out.ErrorClass, got, tc.want)
		}
	}
}
