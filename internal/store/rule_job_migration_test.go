package store_test

import (
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestMigrationDoesNotReplayRestoredLegacyRuleJobs(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, releaseBeforeWorker := range []bool{false, true} {
		name := "gate closed"
		if releaseBeforeWorker {
			name = "gate released"
		}
		t.Run(name, func(t *testing.T) {
			db := template.New(t)
			ctx := t.Context()
			s, err := service.New(db, service.Deps{SecretKey: []byte(strings.Repeat("s", 32))})
			if err != nil {
				t.Fatal(err)
			}
			// This fixture represents a pre-0009 snapshot with a pending rule job.
			// Later migrations are idempotent and rerun with 0009.
			if _, err := db.Pool().Exec(ctx, `INSERT INTO users(id,username,password_hash) VALUES(1,'restore','$argon2id$test');
 INSERT INTO principals(user_id,kind,name) VALUES(1,'human','Human');
 INSERT INTO feeds(id,url) VALUES(1,'https://restore.test/feed');
 INSERT INTO subscriptions(user_id,feed_id) VALUES(1,1);
 INSERT INTO entries(id,feed_id,seq,identity_hash,title) VALUES(1,1,1,decode(repeat('01',32),'hex'),'Restored article');`); err != nil {
				t.Fatal(err)
			}
			actor := service.Actor{UserID: 1, PrincipalID: 1, Kind: service.PrincipalHuman, Via: service.CredentialSession, Scopes: service.HumanScopes}
			rule, err := s.CreateRule(ctx, actor, service.Rule{Name: "Restored", Enabled: true, Predicate: service.RulePredicate{Type: service.RuleFeedIn, IDs: []int64{1}}, Actions: []service.RuleAction{{Type: service.RuleNotify, Budget: 3}}}, "restored-rule")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool().Exec(ctx, `INSERT INTO jobs(kind,payload) VALUES('rule',jsonb_build_object('user_id',1,'rule_id',$1::bigint,'feed_id',1,'entry_ids',jsonb_build_array(1)))`, rule.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool().Exec(ctx, `DELETE FROM schema_migrations WHERE name>='0009_'`); err != nil {
				t.Fatal(err)
			}
			generation, err := db.RotateGeneration(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if releaseBeforeWorker {
				if err := s.ReleaseNotifications(ctx, actor, generation, "release-after-restore"); err != nil {
					t.Fatal(err)
				}
				// A later startup with the gate open must not bind the skipped job.
				if err := db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := s.RunRuleJobs(ctx, 10); err != nil || n != 1 {
				t.Fatalf("jobs=%d err=%v", n, err)
			}
			var notifications, pending int
			if err := db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications),(SELECT count(*) FROM jobs WHERE kind='rule')`).Scan(&notifications, &pending); err != nil {
				t.Fatal(err)
			}
			if notifications != 0 || pending != 0 {
				t.Fatalf("restored job replayed: notifications=%d pending=%d", notifications, pending)
			}
		})
	}
}
