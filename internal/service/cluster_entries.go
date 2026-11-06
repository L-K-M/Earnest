package service

import (
	"context"
)

// ClusterEntries applies read-time cluster suppression to the river. It keeps
// the stored state and sequence cursor unchanged. Suppression runs inside the
// page query itself (EntryFilter.SuppressClusters), so one query returns the
// visible page instead of rescanning suppressed chunks round by round.
func (s *Service) ClusterEntries(ctx context.Context, a Actor, f EntryFilter) ([]Entry, error) {
	if f.View != "" && f.View != ViewRiver {
		return s.Entries(ctx, a, f)
	}
	if f.Limit < 0 || f.Limit > maxEntryLimit {
		return nil, ErrInvalid
	}
	if f.Limit == 0 {
		f.Limit = defaultEntryLimit
	}
	f.SuppressClusters = true
	return s.Entries(ctx, a, f)
}

// ClusterGroups collapses each cluster to one representative per page. The
// representative follows the requested sequence direction; AlsoIn counts all
// other visible subscribed members, including members outside the page.
func (s *Service) ClusterGroups(ctx context.Context, a Actor, f EntryFilter) ([]ClusterGroup, error) {
	limit := f.Limit
	if limit == 0 {
		limit = defaultEntryLimit
	}
	if limit < 1 || limit > maxEntryLimit {
		return nil, ErrInvalid
	}
	scan := f
	scan.Limit = maxEntryLimit
	seen := map[int64]bool{}
	out := []ClusterGroup{}
	for {
		groups, err := s.clusterAnnotations(ctx, a, scan)
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 {
			break
		}
		for _, group := range groups {
			if group.ClusterID != 0 && seen[group.ClusterID] {
				continue
			}
			if group.ClusterID != 0 {
				seen[group.ClusterID] = true
			}
			out = append(out, group)
			if len(out) == limit {
				return out, nil
			}
		}
		if len(groups) < scan.Limit {
			break
		}
		last := groups[len(groups)-1].Entry.Seq
		if scan.Direction == PageBackward {
			scan.BeforeSeq = last
		} else {
			scan.AfterSeq = last
		}
	}
	return out, nil
}
