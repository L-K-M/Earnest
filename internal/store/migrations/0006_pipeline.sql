-- Provider consent belongs to a credentialled feed, never to an agent token.
ALTER TABLE feeds ADD COLUMN llm_allowed boolean NOT NULL DEFAULT false;
ALTER TABLE embeddings ADD COLUMN content_hash bytea CHECK (octet_length(content_hash)=32);
ALTER TABLE embeddings ADD COLUMN input_hash bytea CHECK (octet_length(input_hash)=32);
ALTER TABLE embeddings ADD COLUMN input_title text NOT NULL DEFAULT '';
ALTER TABLE entry_clusters ADD COLUMN reason text NOT NULL DEFAULT 'new_story';
ALTER TABLE entry_clusters ADD COLUMN joined_at timestamptz NOT NULL DEFAULT now();
CREATE UNIQUE INDEX entry_clusters_one_active ON entry_clusters(user_id,entry_id) WHERE deleted_at IS NULL;

CREATE TABLE classifier_results (
 entry_id bigint NOT NULL REFERENCES entries ON DELETE CASCADE,
 model text NOT NULL,
 prompt_version text NOT NULL,
 content_hash bytea NOT NULL CHECK(octet_length(content_hash)=32),
 input_hash bytea NOT NULL CHECK(octet_length(input_hash)=32),
 input_title text NOT NULL,
 labels text[] NOT NULL CHECK(labels <@ ARRAY['press_release','news','opinion','tutorial','research','review','event']::text[]),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(entry_id,model,prompt_version)
);
CREATE TABLE semantic_queries (
 user_id bigint NOT NULL REFERENCES users,
 description text NOT NULL CHECK(length(description) BETWEEN 1 AND 4096),
 model text NOT NULL,
 embedding halfvec(1024) NOT NULL,
 PRIMARY KEY(user_id,description,model)
);
-- Watch configuration extends a normal notify-only semantic rule. The existing
-- ingest transaction and notification budget/deduplication apply unchanged.
CREATE TABLE watch_rules (
 rule_id bigint PRIMARY KEY REFERENCES rules ON DELETE CASCADE,
 check_llm boolean NOT NULL DEFAULT false,
 check_budget integer NOT NULL DEFAULT 3 CHECK(check_budget BETWEEN 1 AND 1000)
);
CREATE TABLE watch_checks (
 rule_id bigint NOT NULL REFERENCES watch_rules ON DELETE CASCADE,
 entry_id bigint NOT NULL REFERENCES entries ON DELETE CASCADE,
 content_hash bytea NOT NULL,
 model text NOT NULL,
 matched boolean,
 attempted_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(rule_id,entry_id,content_hash,model)
);

CREATE TABLE cluster_dones (
 user_id bigint NOT NULL REFERENCES users,
 cluster_id bigint NOT NULL,
 principal_id bigint NOT NULL,
 rev bigint NOT NULL CHECK(rev>0),
 done_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,cluster_id,principal_id),
 FOREIGN KEY(user_id,cluster_id) REFERENCES clusters(user_id,id),
 FOREIGN KEY(user_id,principal_id) REFERENCES principals(user_id,id)
);
-- Cluster's existing deduplication envelope is committed in the same transaction
-- as its per-entry writes. Capture only applied shelf writes at that revision,
-- never individual Done writes or later replay of a completed operation.
CREATE FUNCTION capture_cluster_done() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.result->>'kind' = 'cluster' THEN
  DELETE FROM cluster_dones d USING entry_clusters ec,
   jsonb_array_elements(NEW.result->'results') r
  WHERE d.user_id=ec.user_id AND d.cluster_id=ec.cluster_id AND ec.deleted_at IS NULL
   AND ec.entry_id=(r->'state'->>'entry_id')::bigint
   AND d.principal_id=(r->'state'->>'principal')::bigint
   AND r->>'status'='applied' AND (r->'state'->>'state_rev')::bigint>d.rev
   AND r->'state'->'shelf'->>'value'<>'done'
   AND (r->'state'->'shelf'->>'rev'=r->'state'->>'state_rev'
    OR (r->'state'->'seen'->>'value'='unseen' AND r->'state'->'seen'->>'rev'=r->'state'->>'state_rev'));

  INSERT INTO cluster_dones(user_id,cluster_id,principal_id,rev,done_at)
  SELECT DISTINCT ec.user_id,ec.cluster_id,(r->'state'->>'principal')::bigint,
   (r->'state'->'shelf'->>'rev')::bigint,NEW.created_at
  FROM jsonb_array_elements(NEW.result->'results') r
  JOIN entry_clusters ec ON ec.entry_id=(r->'state'->>'entry_id')::bigint AND ec.deleted_at IS NULL
  JOIN principals p ON p.id=(r->'state'->>'principal')::bigint AND p.user_id=ec.user_id
  WHERE r->>'status'='applied' AND r->'state'->'shelf'->>'value'='done'
   AND r->'state'->'shelf'->>'rev'=r->'state'->>'state_rev'
  ON CONFLICT(user_id,cluster_id,principal_id) DO UPDATE
   SET rev=greatest(cluster_dones.rev,EXCLUDED.rev),done_at=EXCLUDED.done_at;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER cluster_done_receipt AFTER INSERT ON op_ids FOR EACH ROW EXECUTE FUNCTION capture_cluster_done();

-- Preserve pre-pipeline cluster decisions when upgrading an existing database.
-- CREATE TRIGGER holds the write lock while this snapshot is reconstructed.
WITH decisions AS (
 SELECT ec.user_id,ec.cluster_id,p.id AS principal_id,
  (r->'state'->>'state_rev')::bigint AS rev,o.created_at,
  (r->'state'->'shelf'->>'value'='done' AND
   r->'state'->'shelf'->>'rev'=r->'state'->>'state_rev') AS finished
 FROM op_ids o
 CROSS JOIN LATERAL jsonb_array_elements(CASE
  WHEN o.result->>'kind'='cluster' AND jsonb_typeof(o.result->'results')='array'
  THEN o.result->'results' ELSE '[]'::jsonb END) r
 JOIN entry_clusters ec ON ec.entry_id=(r->'state'->>'entry_id')::bigint AND ec.deleted_at IS NULL
 JOIN principals p ON p.id=(r->'state'->>'principal')::bigint AND p.user_id=ec.user_id
 WHERE r->>'status'='applied' AND
  (r->'state'->'shelf'->>'rev'=r->'state'->>'state_rev' OR
   (r->'state'->'seen'->>'value'='unseen' AND
    r->'state'->'seen'->>'rev'=r->'state'->>'state_rev' AND
    r->'state'->'shelf'->>'value'<>'done'))
), latest AS (
 SELECT DISTINCT ON(user_id,cluster_id,principal_id) * FROM decisions
 ORDER BY user_id,cluster_id,principal_id,rev DESC
)
INSERT INTO cluster_dones(user_id,cluster_id,principal_id,rev,done_at)
 SELECT user_id,cluster_id,principal_id,rev,created_at FROM latest WHERE finished;
