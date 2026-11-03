ALTER TABLE rules ADD COLUMN last_match_at timestamptz;
ALTER TABLE rules ADD COLUMN match_count bigint NOT NULL DEFAULT 0 CHECK (match_count >= 0);

-- Entries without an explicit language inherit their feed's search language.
CREATE FUNCTION effective_search_language(entry_language text, feed_language text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN entry_language = 'simple' THEN feed_language ELSE entry_language END
$$;
CREATE OR REPLACE FUNCTION update_entry_search() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE body text;
DECLARE lang text;
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        NEW.search_vector := ''::tsvector;
        RETURN NEW;
    END IF;
    SELECT effective_search_language(NEW.language, language) INTO lang FROM feeds WHERE id = NEW.feed_id;
    SELECT c.text INTO body FROM contents c WHERE c.entry_id = NEW.id AND c.content_hash = NEW.content_hash
        ORDER BY (c.source = 'extracted') DESC, c.sanitizer_version DESC LIMIT 1;
    NEW.search_vector := setweight(to_tsvector(entry_search_config(lang), NEW.title), 'A') ||
        setweight(to_tsvector(entry_search_config(lang), coalesce(body, '')), 'B');
    RETURN NEW;
END $$;
CREATE FUNCTION refresh_feed_search() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.language IS DISTINCT FROM OLD.language THEN
        UPDATE entries SET language = language WHERE feed_id = NEW.id AND language = 'simple';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER feeds_search AFTER UPDATE OF language ON feeds
    FOR EACH ROW EXECUTE FUNCTION refresh_feed_search();
UPDATE entries SET language = language WHERE language = 'simple';

CREATE UNIQUE INDEX principals_rule_actor ON principals(user_id, name) WHERE kind = 'pipeline' AND name = 'Rules';
