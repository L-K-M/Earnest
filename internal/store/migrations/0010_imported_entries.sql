-- Entries stored from another reader's export instead of a fetch. An export
-- cannot say which identity a fetch will derive, so the first fetch that sees
-- the same article adopts the entry instead of storing it twice.
ALTER TABLE entries ADD COLUMN imported boolean NOT NULL DEFAULT false;
CREATE INDEX entries_imported_feed_idx ON entries (feed_id) WHERE imported;
