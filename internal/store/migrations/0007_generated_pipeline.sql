ALTER TABLE contents ADD COLUMN excerpt_only boolean NOT NULL DEFAULT false;
ALTER TABLE annotations ADD COLUMN citations jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(citations)='array');
ALTER TABLE annotations ADD COLUMN content_hash bytea CHECK(content_hash IS NULL OR octet_length(content_hash)=32);
ALTER TABLE annotations ADD COLUMN content_source text;
ALTER TABLE annotations ADD COLUMN sanitizer_version integer;
CREATE UNIQUE INDEX principals_generated ON principals(user_id,name) WHERE kind='pipeline' AND name='Summaries';

CREATE TABLE editions (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users,
 period text NOT NULL CHECK(period IN ('daily','weekly')),
 status text NOT NULL CHECK(status IN ('list','pending','ready','paused','failed')),
 sections jsonb NOT NULL CHECK(jsonb_typeof(sections)='array'),
 model text NOT NULL DEFAULT '',
 prompt_version text NOT NULL,
 ready_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 error text NOT NULL DEFAULT ''
);
CREATE INDEX editions_latest ON editions(user_id,created_at DESC,id DESC);
CREATE TABLE ask_daily_usage (
 user_id bigint NOT NULL REFERENCES users,
 day date NOT NULL,
 attempts integer NOT NULL CHECK(attempts>=0),
 PRIMARY KEY(user_id,day)
);
