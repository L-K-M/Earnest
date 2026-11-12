package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
)

const frontPageSectionLimit = 8

type FrontPage struct {
	Period                             EditionPeriod
	WindowStart, WindowEnd, SelectedAt time.Time
	Revision                           string
	Sections                           []FrontPageSection
}
type FrontPageSection struct {
	Name    string
	Stories []FrontPageStory
}
type FrontPageCoverage struct {
	Entry       Entry
	SourceTitle string
}
type FrontPageStory struct {
	ID                string
	Entry             Entry
	SourceTitle       string
	PublishedAt       time.Time
	FeedCount         int
	Excerpt, ImageURL string
	Coverage          []FrontPageCoverage
	Summary           *FrontPageSummary
}
type FrontPageSummary struct {
	Claims        []pipeline.GroundedClaim `json:"claims"`
	Model         string                   `json:"model"`
	PromptVersion string                   `json:"prompt_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
	ExcerptNotice string                   `json:"excerpt_notice,omitempty"`
}
type frontPageGroup struct {
	id      string
	members []store.FrontPageCandidate
	feeds   map[int64]bool
}

// selectFrontPage is independent of paging, reading state, and provider availability.
func selectFrontPage(candidates []store.FrontPageCandidate) (shared, singles []*frontPageGroup) {
	groups := map[string]*frontPageGroup{}
	for _, c := range candidates {
		id := fmt.Sprintf("entry:%d", c.EntryID)
		if c.ClusterID != 0 {
			id = fmt.Sprintf("cluster:%d", c.ClusterID)
		}
		g := groups[id]
		if g == nil {
			g = &frontPageGroup{id: id, feeds: map[int64]bool{}}
			groups[id] = g
		}
		g.members = append(g.members, c)
		g.feeds[c.FeedID] = true
	}
	newer := func(a, b store.FrontPageCandidate) bool {
		if !a.PublishedAt.Equal(b.PublishedAt) {
			return a.PublishedAt.After(b.PublishedAt)
		}
		return a.EntryID > b.EntryID
	}
	for _, g := range groups {
		sort.Slice(g.members, func(i, j int) bool { return newer(g.members[i], g.members[j]) })
		if len(g.feeds) > 1 {
			shared = append(shared, g)
		} else {
			singles = append(singles, g)
		}
	}
	sort.Slice(shared, func(i, j int) bool {
		if len(shared[i].feeds) != len(shared[j].feeds) {
			return len(shared[i].feeds) > len(shared[j].feeds)
		}
		return newer(shared[i].members[0], shared[j].members[0])
	})
	if len(shared) > frontPageSectionLimit {
		shared = shared[:frontPageSectionLimit]
	}
	sort.Slice(singles, func(i, j int) bool { return newer(singles[i].members[0], singles[j].members[0]) })
	// Each round takes one story per feed, preserving recency within a round.
	remaining := singles
	singles = nil
	for len(remaining) > 0 && len(singles) < frontPageSectionLimit {
		seen := map[int64]bool{}
		next := []*frontPageGroup{}
		for _, g := range remaining {
			f := g.members[0].FeedID
			if seen[f] {
				next = append(next, g)
				continue
			}
			seen[f] = true
			singles = append(singles, g)
			if len(singles) == frontPageSectionLimit {
				break
			}
		}
		remaining = next
	}
	return shared, singles
}

func (s *Service) FrontPage(ctx context.Context, a Actor, period EditionPeriod) (FrontPage, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return FrontPage{}, err
	}
	if period == "" {
		period = EditionDaily
	}
	if period != EditionDaily && period != EditionWeekly {
		return FrontPage{}, ErrInvalid
	}
	now := s.now().UTC()
	window := 24 * time.Hour
	if period == EditionWeekly {
		window *= 7
	}
	out := FrontPage{Period: period, WindowStart: now.Add(-window), WindowEnd: now, SelectedAt: now, Sections: []FrontPageSection{}}
	candidates, err := s.store.FrontPageCandidates(ctx, a.UserID, out.WindowStart, now)
	if err != nil {
		return out, err
	}
	shared, singles := selectFrontPage(candidates)
	for i, groups := range [][]*frontPageGroup{shared, singles} {
		name := "Across your feeds"
		if i == 1 {
			name = "More from your feeds"
		}
		section := FrontPageSection{Name: name, Stories: []FrontPageStory{}}
		for _, g := range groups {
			story, err := s.frontPageStory(ctx, a, g)
			if err != nil {
				return out, err
			}
			if len(story.Coverage) == 0 {
				continue
			}
			section.Stories = append(section.Stories, story)
		}
		if len(section.Stories) > 0 {
			out.Sections = append(out.Sections, section)
		}
	}
	// State and request timestamps must not produce a spurious update banner.
	contentHashes := make(map[int64][]byte, len(candidates))
	for _, candidate := range candidates {
		contentHashes[candidate.EntryID] = candidate.ContentHash
	}
	fingerprint := []any{period}
	for _, section := range out.Sections {
		for _, story := range section.Stories {
			members := []any{}
			for _, m := range story.Coverage {
				members = append(members, []any{m.Entry.ID, m.Entry.Title, m.SourceTitle, m.Entry.PublishedAt, m.Entry.ThumbnailURL, contentHashes[m.Entry.ID]})
			}
			fingerprint = append(fingerprint, []any{story.ID, story.Excerpt, story.ImageURL, story.Summary, members})
		}
	}
	raw, err := json.Marshal(fingerprint)
	if err != nil {
		return out, err
	}
	out.Revision = fmt.Sprintf("%x", sha256.Sum256(raw))
	return out, nil
}

func (s *Service) frontPageStory(ctx context.Context, a Actor, g *frontPageGroup) (FrontPageStory, error) {
	out := FrontPageStory{ID: g.id, Coverage: []FrontPageCoverage{}}
	ids := make([]int64, 0, len(g.members))
	for _, m := range g.members {
		ids = append(ids, m.EntryID)
	}
	entries := map[int64]Entry{}
	for start := 0; start < len(ids); start += maxEntryIDs {
		batch, err := s.getByIDs(ctx, a, ids[start:min(start+maxEntryIDs, len(ids))], false)
		if err != nil {
			return out, err
		}
		for _, e := range batch {
			if !e.Deleted {
				entries[e.ID] = e
			}
		}
	}
	feeds := map[int64]bool{}
	for _, m := range g.members {
		e, ok := entries[m.EntryID]
		if !ok {
			continue
		}
		feeds[m.FeedID] = true
		out.Coverage = append(out.Coverage, FrontPageCoverage{Entry: e, SourceTitle: m.SourceTitle})
		if len(out.Coverage) == 1 {
			out.Entry = e
			out.SourceTitle = m.SourceTitle
			out.PublishedAt = m.PublishedAt
		}
	}
	out.FeedCount = len(feeds)
	if len(out.Coverage) == 0 {
		return out, nil
	}
	source, err := s.store.SummaryContent(ctx, a.UserID, out.Entry.ID)
	if err != nil && !isMissingFrontSource(err) {
		return out, err
	}
	if err == nil {
		out.Excerpt = frontPageExcerpt(source.Text)
	}
	out.ImageURL, err = s.frontPageImageURL(ctx, out.Entry.ID)
	if err != nil {
		return out, err
	}
	out.Summary, err = s.frontPageSummary(ctx, a, out.Coverage)
	return out, err
}

func frontPageExcerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) <= 320 {
		return text
	}
	text = string(r[:320])
	if i := strings.LastIndex(text, " "); i > 240 {
		text = text[:i]
	}
	return text + "…"
}
