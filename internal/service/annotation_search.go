package service

import (
	"context"
	"fmt"
)

// AnnotationSearchHit labels a note with its author. These results are for
// user/agent search only and must never become internal model input.
type AnnotationSearchHit struct {
	Annotation
	AuthorKind PrincipalKind `json:"author_kind"`
}

// SearchAnnotations searches notes without promoting their text into entry content.
func (s *Service) SearchAnnotations(ctx context.Context, a Actor, query string, limit int) ([]AnnotationSearchHit, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 100
	}
	if len(query) == 0 || len(query) > 1024 || limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT n.id,n.principal_id,n.kind,n.entry_ids,n.value,n.rev,p.name,p.kind,n.created_at
 FROM annotations n JOIN principals p ON p.id=n.principal_id
 WHERE n.user_id=$1 AND n.deleted_at IS NULL AND n.kind='note'
 AND to_tsvector('simple',n.value) @@ websearch_to_tsquery('simple',$2)
 ORDER BY n.id DESC LIMIT $3`, a.UserID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search annotations: %w", err)
	}
	defer rows.Close()
	out := []AnnotationSearchHit{}
	for rows.Next() {
		var h AnnotationSearchHit
		if err := rows.Scan(&h.ID, &h.PrincipalID, &h.Kind, &h.EntryIDs, &h.Value, &h.Rev, &h.AuthorName, &h.AuthorKind, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Untrusted = a.Kind == PrincipalAgent && h.PrincipalID != a.PrincipalID
		out = append(out, h)
	}
	return out, rows.Err()
}
