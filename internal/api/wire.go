package api

import (
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

// Transport DTOs keep JSON names stable without adding transport tags to service.
type entry struct {
	ID             int64           `json:"id"`
	SubscriptionID int64           `json:"subscription_id"`
	FeedID         int64           `json:"feed_id"`
	Seq            uint64          `json:"seq"`
	FirstSeenAt    time.Time       `json:"first_seen_at"`
	PublishedAt    *time.Time      `json:"published_at,omitempty"`
	UpdatedAt      *time.Time      `json:"updated_at,omitempty"`
	Title          string          `json:"title"`
	Blurb          *string         `json:"blurb,omitempty"`
	URL            string          `json:"url"`
	Author         string          `json:"author"`
	Rev            uint64          `json:"rev"`
	Deleted        bool            `json:"deleted"`
	State          syncmodel.State `json:"state"`
}
type content struct {
	EntryID          int64                 `json:"entry_id"`
	HTML             string                `json:"html"`
	Text             string                `json:"text"`
	SanitizerVersion int                   `json:"sanitizer_version"`
	Source           service.ContentSource `json:"source"`
}
type token struct {
	ID          int64             `json:"id"`
	PrincipalID int64             `json:"principal_id"`
	Name        string            `json:"name"`
	Kind        service.TokenKind `json:"kind"`
	Audience    service.Audience  `json:"audience"`
	Scopes      []service.Scope   `json:"scopes"`
	DailyBudget int               `json:"daily_budget"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time        `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time        `json:"revoked_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}
type tokenSecret struct {
	Token  token  `json:"token"`
	Secret string `json:"secret"`
}

func toEntry(e service.Entry) entry {
	return entry{ID: e.ID, SubscriptionID: e.SubscriptionID, FeedID: e.FeedID, Seq: e.Seq, FirstSeenAt: e.FirstSeenAt, PublishedAt: e.PublishedAt, UpdatedAt: e.UpdatedAt, Title: e.Title, Blurb: e.Blurb, URL: e.URL, Author: e.Author, Rev: e.Rev, Deleted: e.Deleted, State: e.State}
}
func toEntries(items []service.Entry) []entry {
	result := make([]entry, 0, len(items))
	for _, item := range items {
		result = append(result, toEntry(item))
	}
	return result
}
