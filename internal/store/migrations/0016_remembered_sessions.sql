-- A remembered session keeps one browser signed in across restarts and
-- longer gaps between visits; its limits are set by the service.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS remembered boolean NOT NULL DEFAULT false;
