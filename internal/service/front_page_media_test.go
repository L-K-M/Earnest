package service

import (
	"bytes"
	"image"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestFrontPageStoredFeatureImage(t *testing.T) {
	p := newMediaPublisher(t)
	f := mediaTestNew(t, storetest.NewTemplate(t), p)
	ctx := t.Context()
	feed := entriesTestFeed(t, f.db, p.URL+"/rss", 0)
	entriesTestSub(t, f.db, f.a.UserID, feed, "today")
	f.record(t, feed, nil, mediaItem("photo", p.URL+"/photo.jpg", ""))
	id := f.entryID(t, feed, "photo")
	if _, err := f.s.RunMedia(ctx, 10); err != nil {
		t.Fatal(err)
	}
	before := p.hit("/photo.jpg")
	file, err := f.s.EntryImage(ctx, f.a, id)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(file.Body))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 120 {
		t.Fatalf("cropped/upscaled %v", img.Bounds())
	}
	page, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sections) != 1 || page.Sections[0].Stories[0].ImageURL == "" {
		t.Fatalf("missing image %+v", page)
	}
	if p.hit("/photo.jpg") != before {
		t.Fatal("view fetched publisher image")
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE subscriptions SET deleted_at=now() WHERE feed_id=$1`, feed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.EntryImage(ctx, f.a, id); err == nil {
		t.Fatal("image survives removed subscription")
	}
}
