package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Service) frontPageImageURL(ctx context.Context, id int64) (string, error) {
	var etag string
	err := s.store.Pool().QueryRow(ctx, `SELECT feature_etag FROM entry_images WHERE entry_id=$1 AND feature_body IS NOT NULL`, id).Scan(&etag)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/v1/entries/%d/image?v=%s", id, url.QueryEscape(etag)), nil
}

// EntryImage authorizes exactly as the existing thumbnail endpoint does.
func (s *Service) EntryImage(ctx context.Context, a Actor, id int64) (MediaFile, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return MediaFile{}, err
	}
	if id <= 0 {
		return MediaFile{}, ErrInvalid
	}
	visible, err := store.LoadEntryRows(ctx, s.store.Pool(), a.UserID, []int64{id})
	if err != nil {
		return MediaFile{}, err
	}
	if len(visible) == 0 || visible[0].Deleted {
		return MediaFile{}, ErrNotFound
	}
	var out MediaFile
	err = s.store.Pool().QueryRow(ctx, `SELECT feature_content_type,feature_body,feature_etag FROM entry_images WHERE entry_id=$1 AND feature_body IS NOT NULL`, id).Scan(&out.ContentType, &out.Body, &out.ETag)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}
