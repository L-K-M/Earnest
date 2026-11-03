package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func TestHTTPRiverHidesLateClusterMemberWithoutChangingState(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	if _, err := f.svc.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	var cluster int64
	if err := f.db.Pool().QueryRow(ctx, `SELECT cluster_id FROM entry_clusters WHERE user_id=$1 AND entry_id=$2`, f.owner.UserID, f.entryID).Scan(&cluster); err != nil {
		t.Fatal(err)
	}
	generation, err := f.db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.Cluster(ctx, f.owner, cluster, syncmodel.Write{OpID: "finish-story", Principal: syncmodel.ID(f.owner.PrincipalID), Register: syncmodel.Shelf, Value: syncmodel.Done, Base: syncmodel.Revision{Generation: generation}, OccurredAt: time.Now().UTC(), Kind: syncmodel.Explicit})
	if err != nil {
		t.Fatal(err)
	}
	// fixtureProber supplies the same canonical story in a different feed.
	if _, err := f.svc.Subscribe(ctx, f.owner, "https://example.org/another-feed", service.SubscribeOptions{OpID: "late-story"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, http.MethodGet, "/v1/entries?view=river", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	page := decodeBody[entriesPage](t, rec)
	if len(page.Entries) != 0 {
		t.Fatalf("late duplicate in river: %+v", page)
	}
	rec = f.do(t, http.MethodGet, "/v1/entries?view=all", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	page = decodeBody[entriesPage](t, rec)
	if len(page.Entries) != 2 {
		t.Fatalf("all view lost entry: %+v", page)
	}
	rec = f.do(t, http.MethodGet, "/v1/entries?view=all&group=cluster", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	page = decodeBody[entriesPage](t, rec)
	if len(page.Groups) != 1 || page.Groups[0].AlsoIn != 1 || page.Status != statusDisabled {
		t.Fatalf("grouped story %+v", page)
	}
}
