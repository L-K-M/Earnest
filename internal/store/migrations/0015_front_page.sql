-- Cached generated prose is scoped to its user's exact source versions.
CREATE TABLE IF NOT EXISTS front_page_summaries (
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 cache_key text NOT NULL,
 result jsonb,
 retry_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(user_id,cache_key)
);
ALTER TABLE entry_images ADD COLUMN IF NOT EXISTS feature_body bytea;
ALTER TABLE entry_images ADD COLUMN IF NOT EXISTS feature_content_type text CHECK (feature_content_type IN ('image/png','image/jpeg'));
ALTER TABLE entry_images ADD COLUMN IF NOT EXISTS feature_etag text;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='entry_images'::regclass AND conname='feature_image_complete') THEN
  ALTER TABLE entry_images ADD CONSTRAINT feature_image_complete CHECK ((feature_body IS NULL)=(feature_etag IS NULL) AND (feature_body IS NULL)=(feature_content_type IS NULL));
 END IF;
END $$;
-- Existing images receive the larger derivative through the bounded media worker.
UPDATE entry_images SET status='pending',next_attempt_at=now(),attempts=0
WHERE feature_body IS NULL AND cardinality(candidates)>0 AND entry_id IN
 (SELECT id FROM entries WHERE deleted_at IS NULL AND coalesce(published_at,first_seen_at)>=now()-interval '7 days');
CREATE INDEX IF NOT EXISTS entries_publication_window ON entries ((coalesce(published_at,first_seen_at))) WHERE deleted_at IS NULL AND NOT imported;
