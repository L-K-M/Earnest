package api

import (
	"context"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHintsOnlyAfterCommit(t *testing.T) {
	template := storetest.NewTemplate(t)
	db := template.New(t)
	ctx := context.Background()
	key := []byte(strings.Repeat("x", 32))
	svc, err := service.New(db, service.Deps{SecretKey: key})
	if err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuth(db, nil, key)
	owner, err := auth.CreateUser(ctx, "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := auth.CreateUser(ctx, "other", "password")
	if err != nil {
		t.Fatal(err)
	}
	hints, cancel, err := svc.WatchChanges(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	otherHints, cancelOther, err := svc.WatchChanges(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOther()
	// Fail after the event insert, proving a staged event never escapes rollback.
	_, err = db.Pool().Exec(ctx, `CREATE FUNCTION reject_test_op() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.op_id='rollback' THEN RAISE EXCEPTION 'test rollback'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_test_op BEFORE INSERT ON op_ids FOR EACH ROW EXECUTE FUNCTION reject_test_op()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateFolder(ctx, owner, "rolled back", "rollback"); err == nil {
		t.Fatal("expected injected rollback")
	}
	select {
	case <-hints:
		t.Fatal("hint escaped rollback")
	default:
	}
	folders, err := svc.Folders(ctx, owner)
	if err != nil || len(folders) != 0 {
		t.Fatalf("rollback folders = %v, %v", folders, err)
	}
	if _, err := svc.CreateFolder(ctx, owner, "committed", "success"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
	default:
		t.Fatal("committed change did not publish")
	}
	select {
	case <-otherHints:
		t.Fatal("another user's hint leaked")
	default:
	}
	// Successful operation replays do not commit another event.
	if _, err := svc.CreateFolder(ctx, owner, "committed", "success"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
		t.Fatal("replay published another hint")
	default:
	}
	cancel()
	cancel()
	if _, err := svc.CreateFolder(ctx, owner, "after cancel", "later"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
		t.Fatal("cancelled listener received hint")
	default:
	}
}
