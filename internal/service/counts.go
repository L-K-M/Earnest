package service

import (
	"context"
	"fmt"
)

// FeedUnreadCount is one subscription's unread tallies. Later and kept are
// subsets of Unread: they count unread entries on those shelves.
type FeedUnreadCount struct {
	SubscriptionID int64 `json:"subscription_id"`
	Unread         int   `json:"unread"`
	Later          int   `json:"later"`
	Kept           int   `json:"kept"`
}

// UnreadCounts is the account-wide unread tallies behind unread-count mode.
type UnreadCounts struct {
	Feeds []FeedUnreadCount `json:"feeds"`
	Later int               `json:"later"`
	Kept  int               `json:"kept"`
}

// UnreadCounts tallies unread entries per subscription in one grouped query.
// The semantics match listing view=all with unread_only: entries that are not
// retention tombstones and whose state is unseen and not Done, resolved
// against the same principal the entry list uses. Clients previously paged
// every unread entry to build these totals; the database can aggregate them.
func (s *Service) UnreadCounts(ctx context.Context, a Actor) (UnreadCounts, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return UnreadCounts{}, err
	}
	principal, err := s.statePrincipal(ctx, a, 0)
	if err != nil {
		return UnreadCounts{}, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT s.id,
 count(*) FILTER (WHERE COALESCE(st.seen,'unseen')='unseen' AND COALESCE(st.shelf,'none')<>'done'),
 count(*) FILTER (WHERE COALESCE(st.seen,'unseen')='unseen' AND st.shelf='later'),
 count(*) FILTER (WHERE COALESCE(st.seen,'unseen')='unseen' AND COALESCE(st.shelf,'none')<>'done' AND st.kept)
 FROM entries e
 JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL
 LEFT JOIN entry_state st ON st.principal_id=$2 AND st.entry_id=e.id
 WHERE e.deleted_at IS NULL
 GROUP BY s.id ORDER BY s.id`, a.UserID, principal)
	if err != nil {
		return UnreadCounts{}, fmt.Errorf("unread counts: %w", err)
	}
	defer rows.Close()
	out := UnreadCounts{Feeds: []FeedUnreadCount{}}
	for rows.Next() {
		var f FeedUnreadCount
		if err := rows.Scan(&f.SubscriptionID, &f.Unread, &f.Later, &f.Kept); err != nil {
			return UnreadCounts{}, fmt.Errorf("scan unread counts: %w", err)
		}
		out.Later += f.Later
		out.Kept += f.Kept
		out.Feeds = append(out.Feeds, f)
	}
	return out, rows.Err()
}
