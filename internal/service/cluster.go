package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const (
	// maxClusterPending bounds one user's scan per pass.
	maxClusterPending = 500
	// clusterCosineMaxDistance is 1 - 0.88: pgvector returns cosine distance.
	clusterCosineMaxDistance = 0.12
	// clusterSimilarLimit bounds the candidate rows scored per entry.
	clusterSimilarLimit = 50
	// clusterFeedCap is the maximum members one feed contributes to a cluster.
	clusterFeedCap = 2
	// clusterMatchWindow is how far back cluster activity stays a join target.
	clusterMatchWindow = 7 * 24 * time.Hour
	// clusterTitleMinTokens is the minimum for a token-overlap title match.
	clusterTitleMinTokens = 4
	// clusterTitleJaccard is the token-set threshold for a near-duplicate title.
	clusterTitleJaccard = 0.8
)

// ClusterReason records why an entry joined its cluster, persisted on the
// membership row so readers can distinguish a new story from a same-URL or
// title join.
type ClusterReason string

const (
	ClusterNewStory  ClusterReason = "new_story"
	ClusterSameURL   ClusterReason = "same_url"
	ClusterSameTitle ClusterReason = "same_title"
	ClusterSimilar   ClusterReason = "similar"
)

// ClusterPending assigns each unclustered entry visible through an active
// subscription to one story cluster per user. Entries join in ascending seq:
// first by exact canonical URL, then near-duplicate title, then — only when
// model names the embed task's model — cosine distance at or below 0.12.
// Every path compares against clusters that accepted a member in the last
// seven days by server ingest time, caps one feed at two members and falls back to a new cluster.
// model "" runs deterministic matching only, which keeps private feeds
// clusterable without provider consent. Each user runs under a transaction
// advisory lock, so overlapping passes serialize instead of racing.
// limit bounds each user's scan; the return counts entries assigned.
func (s *Service) ClusterPending(ctx context.Context, model string, limit int) (int, error) {
	if limit <= 0 || limit > maxClusterPending {
		return 0, ErrInvalid
	}
	users, err := store.PendingClusterUsers(ctx, s.store.Pool())
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, user := range users {
		var n int
		err := s.withEvents(ctx, func(tx pgx.Tx) error {
			var err error
			n, err = s.clusterUser(ctx, tx, user, model, limit)
			return err
		})
		processed += n
		if err != nil {
			return processed, err
		}
	}
	return processed, nil
}

func (s *Service) clusterUser(ctx context.Context, tx pgx.Tx, user int64, model string, limit int) (int, error) {
	if err := store.LockClusterPass(ctx, tx, user); err != nil {
		return 0, err
	}
	if err := store.LockCore(ctx, tx); err != nil {
		return 0, err
	}
	pending, err := store.PendingClusterEntries(ctx, tx, user, limit)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	now := s.now().UTC()
	oldest := pending[0].FirstSeenAt
	for _, entry := range pending {
		if entry.FirstSeenAt.Before(oldest) {
			oldest = entry.FirstSeenAt
		}
	}
	members, err := store.RecentClusterMembers(ctx, tx, user, oldest.Add(-clusterMatchWindow))
	if err != nil {
		return 0, err
	}
	ix := newClusterIndex(members)
	processed := 0
	for _, p := range pending {
		cluster, reason := ix.match(p)
		if cluster == 0 && model != "" {
			waiting, err := store.ClusterVectorPending(ctx, tx, p.ID, model)
			if err != nil {
				return processed, err
			}
			if waiting {
				continue
			}

			similar, err := store.SimilarClusterEntries(ctx, tx, user, p.ID, model, ix.entryIDs(p.FeedID, p.FirstSeenAt), clusterCosineMaxDistance, clusterSimilarLimit)
			if err != nil {
				return processed, err
			}
			for _, hit := range similar {
				if candidate := ix.clusterOf(hit.EntryID); candidate != 0 && ix.joinable(candidate, p.FeedID) {
					cluster, reason = candidate, ClusterSimilar
					break
				}
			}
		}
		rev, err := changeRev(ctx, tx, user)
		if err != nil {
			return processed, err
		}
		if cluster == 0 {
			reason = ClusterNewStory
			cluster, err = store.InsertCluster(ctx, tx, user, p.Title, rev)
		} else {
			err = store.TouchCluster(ctx, tx, user, cluster, rev)
		}
		if err != nil {
			return processed, err
		}
		if err := store.UpsertClusterMember(ctx, tx, user, cluster, p.ID, rev, string(reason), now); err != nil {
			return processed, err
		}
		event := map[string]any{"id": cluster, "entry_id": p.ID, "reason": string(reason), "rev": rev, "deleted": false}
		if err := s.emitChange(ctx, tx, user, rev, syncmodel.ChangeType("cluster.updated"), "cluster", event); err != nil {
			return processed, err
		}
		ix.add(cluster, p, now)
		processed++
	}
	return processed, nil
}

// clusterIndex is the in-memory candidate set for one user's pass: active
// members of still-recent clusters, plus every join made so far this pass so
// later pending entries see the same deterministic state the database will.
type clusterIndex struct {
	members []clusterCandidate
	byEntry map[int64]int64
	feeds   map[int64]map[int64]int
}

type clusterCandidate struct {
	member store.ClusterMember
	key    string
	core   string
	tokens map[string]struct{}
}

func newClusterIndex(ms []store.ClusterMember) *clusterIndex {
	ix := &clusterIndex{byEntry: map[int64]int64{}, feeds: map[int64]map[int64]int{}}
	for _, m := range ms {
		ix.addMember(m)
	}
	return ix
}

func (ix *clusterIndex) addMember(m store.ClusterMember) {
	m.URL = feed.CleanURL(m.URL)
	ix.members = append(ix.members, clusterCandidate{
		member: m,
		key:    clusterTitleKey(m.Title),
		core:   clusterTitleKey(clusterTitleCore(m.Title)),
		tokens: clusterTitleSet(m.Title),
	})
	ix.byEntry[m.EntryID] = m.ClusterID
	if ix.feeds[m.ClusterID] == nil {
		ix.feeds[m.ClusterID] = map[int64]int{}
	}
	ix.feeds[m.ClusterID][m.FeedID]++
}

// match runs the deterministic stages in priority order. Candidates compete
// inside a stage by most recent join, tie-broken on the lowest cluster id,
// and every candidate still has to leave headroom under the per-feed cap.
func (ix *clusterIndex) match(p store.PendingClusterEntry) (int64, ClusterReason) {
	p.URL = feed.CleanURL(p.URL)
	if p.URL != "" {
		if c := ix.best(func(m clusterCandidate) bool {
			return m.member.URL == p.URL && ix.joinable(m.member.ClusterID, p.FeedID) && clusterWithinWindow(m.member.FirstSeenAt, p.FirstSeenAt)
		}); c != 0 {
			return c, ClusterSameURL
		}
	}
	key, core, tokens := clusterTitleKey(p.Title), clusterTitleKey(clusterTitleCore(p.Title)), clusterTitleSet(p.Title)
	if key != "" {
		if c := ix.best(func(m clusterCandidate) bool {
			return clusterTitleNear(m, key, core, tokens) && ix.joinable(m.member.ClusterID, p.FeedID) && clusterWithinWindow(m.member.FirstSeenAt, p.FirstSeenAt)
		}); c != 0 {
			return c, ClusterSameTitle
		}
	}
	return 0, ""
}

// best returns the cluster of the preferred matching member: the one that
// joined most recently, lowest cluster id on a tie.
func (ix *clusterIndex) best(ok func(clusterCandidate) bool) int64 {
	best := int64(0)
	var at time.Time
	for _, m := range ix.members {
		if !ok(m) {
			continue
		}
		if best == 0 || m.member.JoinedAt.After(at) || (m.member.JoinedAt.Equal(at) && m.member.ClusterID < best) {
			best, at = m.member.ClusterID, m.member.JoinedAt
		}
	}
	return best
}

func (ix *clusterIndex) joinable(cluster, feed int64) bool {
	return ix.feeds[cluster][feed] < clusterFeedCap
}

func (ix *clusterIndex) clusterOf(entry int64) int64 {
	return ix.byEntry[entry]
}

func (ix *clusterIndex) entryIDs(feed int64, at time.Time) []int64 {
	ids := make([]int64, 0, len(ix.byEntry))
	for _, candidate := range ix.members {
		m := candidate.member
		if ix.joinable(m.ClusterID, feed) && clusterWithinWindow(m.FirstSeenAt, at) {
			ids = append(ids, m.EntryID)
		}
	}

	slices.Sort(ids)
	return ids
}

func (ix *clusterIndex) add(cluster int64, p store.PendingClusterEntry, at time.Time) {
	ix.addMember(store.ClusterMember{
		ClusterID:   cluster,
		EntryID:     p.ID,
		FeedID:      p.FeedID,
		Title:       p.Title,
		URL:         p.URL,
		JoinedAt:    at,
		FirstSeenAt: p.FirstSeenAt,
	})
}

// clusterTitleTokens normalises a title to lowercase word tokens, treating
// every run of non-letters and non-digits as one separator.
func clusterTitleTokens(title string) []string {
	return strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func clusterTitleSet(title string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range clusterTitleTokens(title) {
		out[t] = struct{}{}
	}
	return out
}

func clusterTitleKey(title string) string {
	return strings.Join(clusterTitleTokens(title), " ")
}

// clusterTitleCore drops a short trailing outlet segment such as
// " - BBC News" or " | Reuters" so the same syndicated story compares equal
// across sources. The cut only applies when the suffix stays bounded; a long
// tail is treated as content, not branding.
func clusterTitleCore(title string) string {
	cut := len(title)
	for _, sep := range []string{" - ", " | ", " – ", " — "} {
		if i := strings.LastIndex(title, sep); i > 0 && len(title)-i-len(sep) <= 60 {
			cut = min(cut, i)
		}
	}
	return title[:cut]
}

// clusterTitleNear reports a near duplicate: equal after token normalisation,
// equal after stripping an outlet suffix, or a token-set overlap above the
// Jaccard threshold.
func clusterTitleNear(m clusterCandidate, key, core string, tokens map[string]struct{}) bool {
	if key == "" || m.key == "" {
		return false
	}
	if key == m.key || (core != "" && core == m.core) {
		return true
	}
	if len(tokens) < clusterTitleMinTokens || len(m.tokens) < clusterTitleMinTokens {
		return false
	}
	both := 0
	for t := range tokens {
		if _, ok := m.tokens[t]; ok {
			both++
		}
	}
	union := len(tokens) + len(m.tokens) - both
	return union > 0 && float64(both)/float64(union) >= clusterTitleJaccard
}

// ClusterGroup annotates one listed entry with its cluster identity and how
// many other visible members share it ("also in N").
type ClusterGroup struct {
	Entry     Entry
	ClusterID int64
	AlsoIn    int
}

// ClusterGroups lists the filtered page with cluster membership resolved per
// entry, in the same order Entries returns it. Entries without an active
// cluster carry zero values.
func (s *Service) clusterAnnotations(ctx context.Context, a Actor, f EntryFilter) ([]ClusterGroup, error) {
	entries, err := s.ClusterEntries(ctx, a, f)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	memberships, err := store.ClusterMemberships(ctx, s.store.Pool(), a.UserID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ClusterGroup, len(entries))
	for i, e := range entries {
		out[i] = ClusterGroup{Entry: e}
		if m, ok := memberships[e.ID]; ok {
			out[i].ClusterID = m.ClusterID
			out[i].AlsoIn = m.AlsoIn
		}
	}
	return out, nil
}

// ClusterSuppressed reports which of ids stay out of the undecided flow as
// late members of a cluster the principal finished through Service.Cluster.
// It is read-time only: the stored registers stay unseen, and a state write
// newer than the done mark — such as an explicit unread — lifts suppression.
func (s *Service) ClusterSuppressed(ctx context.Context, a Actor, principal int64, ids []int64) (map[int64]bool, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	principal, err := s.statePrincipal(ctx, a, principal)
	if err != nil {
		return nil, err
	}
	if len(ids) > maxEntryIDs {
		return nil, fmt.Errorf("more than %d entry ids: %w", maxEntryIDs, ErrInvalid)
	}
	out := make(map[int64]bool, len(ids))
	want := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("entry id %d: %w", id, ErrInvalid)
		}
		out[id] = false
		if !seen[id] {
			seen[id] = true
			want = append(want, id)
		}
	}
	suppressed, err := store.SuppressedClusterEntries(ctx, s.store.Pool(), a.UserID, principal, want)
	if err != nil {
		return nil, err
	}
	for _, id := range suppressed {
		out[id] = true
	}
	return out, nil
}

func clusterWithinWindow(candidate, entry time.Time) bool {
	return !candidate.Before(entry.Add(-clusterMatchWindow)) && !candidate.After(entry)
}
