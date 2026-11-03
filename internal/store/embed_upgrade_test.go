package store

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPipelineUpgradePreservesClusterDecisions(t *testing.T) {
	db := migrationDatabase(t)
	ctx := context.Background()
	files := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Compare(entry.Name(), "0006_") >= 0 {
			continue
		}
		name := "migrations/" + entry.Name()
		body, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: body}
	}
	if err := db.migrate(ctx, files); err != nil {
		t.Fatal(err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO users(id,username,password_hash) VALUES(1,'upgrade','$argon2id$test');
 INSERT INTO principals(id,user_id,kind,name) VALUES(1,1,'human','Human');
 INSERT INTO feeds(id,url) VALUES(1,'https://upgrade.test/feed');
 INSERT INTO entries(id,feed_id,seq,identity_hash) VALUES(1,1,1,decode(repeat('01',32),'hex')),(2,1,2,decode(repeat('02',32),'hex'));
 INSERT INTO clusters(id,user_id,rev) VALUES(1,1,1),(2,1,1);
 INSERT INTO entry_clusters(user_id,cluster_id,entry_id,rev) VALUES(1,1,1,2),(1,2,2,2);
 INSERT INTO op_ids(principal_id,op_id,result) VALUES
 (1,'old-finish','{"kind":"cluster","results":[{"status":"applied","state":{"principal":1,"entry_id":1,"shelf":{"value":"done","rev":3},"state_rev":3}}]}'),
 (1,'old-finish-then-reopen','{"kind":"cluster","results":[{"status":"applied","state":{"principal":1,"entry_id":2,"shelf":{"value":"done","rev":4},"state_rev":4}}]}'),
 (1,'old-reopen','{"kind":"cluster","results":[{"status":"applied","state":{"principal":1,"entry_id":2,"seen":{"value":"unseen","rev":5},"shelf":{"value":"none","rev":5},"state_rev":5}}]}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count, rev int
	if err := db.pool.QueryRow(ctx, `SELECT count(*),coalesce(max(rev),0) FROM cluster_dones WHERE user_id=1`).Scan(&count, &rev); err != nil || count != 1 || rev != 3 {
		t.Fatalf("upgrade lost cluster decision: count=%d rev=%d error=%v", count, rev, err)
	}
}
