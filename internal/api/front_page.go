package api

import (
	"context"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
)

type frontPageInput struct {
	Period service.EditionPeriod `query:"period" default:"daily" enum:"daily,weekly"`
}
type frontPage struct {
	Period      service.EditionPeriod `json:"period"`
	WindowStart time.Time             `json:"window_start"`
	WindowEnd   time.Time             `json:"window_end"`
	SelectedAt  time.Time             `json:"selected_at"`
	Revision    string                `json:"revision"`
	Sections    []frontPageSection    `json:"sections"`
}
type frontPageSection struct {
	Name    string           `json:"name"`
	Stories []frontPageStory `json:"stories"`
}
type frontPageCoverage struct {
	Entry       entry  `json:"entry"`
	SourceTitle string `json:"source_title"`
}
type frontPageStory struct {
	ID          string                    `json:"id"`
	Entry       entry                     `json:"entry"`
	SourceTitle string                    `json:"source_title"`
	PublishedAt time.Time                 `json:"published_at"`
	FeedCount   int                       `json:"feed_count"`
	Excerpt     string                    `json:"excerpt"`
	ImageURL    string                    `json:"image_url,omitempty"`
	Coverage    []frontPageCoverage       `json:"coverage"`
	Summary     *service.FrontPageSummary `json:"summary,omitempty"`
}

func (s *server) frontPageRoutes() {
	route(s, http.MethodGet, "/v1/front-page", "front-page", func(ctx context.Context, in *frontPageInput) (frontPage, error) {
		page, err := s.svc.FrontPage(ctx, actor(ctx), in.Period)
		if err != nil {
			return frontPage{}, err
		}
		out := frontPage{Period: page.Period, WindowStart: page.WindowStart, WindowEnd: page.WindowEnd, SelectedAt: page.SelectedAt, Revision: page.Revision, Sections: []frontPageSection{}}
		for _, section := range page.Sections {
			sec := frontPageSection{Name: section.Name, Stories: []frontPageStory{}}
			for _, story := range section.Stories {
				st := frontPageStory{ID: story.ID, Entry: toEntry(story.Entry), SourceTitle: story.SourceTitle, PublishedAt: story.PublishedAt, FeedCount: story.FeedCount, Excerpt: story.Excerpt, ImageURL: story.ImageURL, Summary: story.Summary, Coverage: []frontPageCoverage{}}
				for _, c := range story.Coverage {
					st.Coverage = append(st.Coverage, frontPageCoverage{Entry: toEntry(c.Entry), SourceTitle: c.SourceTitle})
				}
				sec.Stories = append(sec.Stories, st)
			}
			out.Sections = append(out.Sections, sec)
		}
		return out, nil
	})
}
