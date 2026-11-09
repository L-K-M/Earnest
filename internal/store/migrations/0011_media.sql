-- Feed icons and article thumbnails are downloaded when feeds and articles
-- arrive, never when a list is viewed, so showing a list cannot tell a
-- publisher which articles a person or an agent picked (rule 7 in
-- docs/research/llm.md). Each row is also the media worker's work item: it
-- claims rows whose next_attempt_at has passed. Bodies are small re-encoded
-- copies (internal/media), never the publisher's bytes.

-- One icon per feed. declared_url (the feed document's own image) and
-- site_url are the inputs of the last resolution; ingest reschedules the row
-- when either changes. A ready row is refreshed after a month.
CREATE TABLE IF NOT EXISTS feed_icons (
    feed_id bigint PRIMARY KEY REFERENCES feeds ON DELETE CASCADE,
    declared_url text CHECK (declared_url ~ '^https?://'),
    site_url text,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'none')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    source_url text,
    content_type text CHECK (content_type IN ('image/png', 'image/jpeg')),
    body bytea,
    etag text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((body IS NULL) = (etag IS NULL) AND (body IS NULL) = (content_type IS NULL))
);
CREATE INDEX IF NOT EXISTS feed_icons_due ON feed_icons (next_attempt_at);

-- One thumbnail per entry. candidates are the image URLs chosen at ingest,
-- best first; 'none' means the article offered none and 'failed' that every
-- candidate failed for good. A body stays servable while a changed article is
-- pending again. Retention deletes the row with the entry's contents.
CREATE TABLE IF NOT EXISTS entry_images (
    entry_id bigint PRIMARY KEY REFERENCES entries ON DELETE CASCADE,
    candidates text[] NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'ready', 'none', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    source_url text,
    content_type text CHECK (content_type IN ('image/png', 'image/jpeg')),
    body bytea,
    etag text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((body IS NULL) = (etag IS NULL) AND (body IS NULL) = (content_type IS NULL)),
    CHECK (status <> 'none' OR cardinality(candidates) = 0)
);
CREATE INDEX IF NOT EXISTS entry_images_pending ON entry_images (entry_id) WHERE status = 'pending';
