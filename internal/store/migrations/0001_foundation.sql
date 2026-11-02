CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE users (
    id bigserial PRIMARY KEY,
    username text NOT NULL UNIQUE CHECK (btrim(username) <> ''),
    password_hash text NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
    settings jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(settings) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE principals (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    kind text NOT NULL CHECK (kind IN ('human', 'agent', 'pipeline')),
    name text NOT NULL CHECK (btrim(name) <> ''),
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id)
);
CREATE INDEX principals_user ON principals (user_id, id);
CREATE UNIQUE INDEX principals_human ON principals (user_id) WHERE kind = 'human';
CREATE TABLE sessions (
    id_hash bytea PRIMARY KEY CHECK (octet_length(id_hash) = 32),
    principal_id bigint NOT NULL REFERENCES principals,
    csrf_secret bytea NOT NULL CHECK (octet_length(csrf_secret) >= 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX sessions_principal ON sessions (principal_id);
CREATE INDEX sessions_expiry ON sessions (expires_at);
CREATE TABLE tokens (
    id bigserial PRIMARY KEY,
    principal_id bigint NOT NULL REFERENCES principals,
    name text NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    kind text NOT NULL CHECK (kind IN ('app', 'agent', 'compat')),
    audience text NOT NULL CHECK (audience IN ('api', 'mcp')),
    scopes text[] NOT NULL DEFAULT '{}',
    daily_budget integer NOT NULL DEFAULT 3 CHECK (daily_budget >= 0),
    expires_at timestamptz,
    last_used_at timestamptz,
    revoked_at timestamptz,
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at IS NULL OR expires_at > created_at)
);
CREATE INDEX tokens_principal ON tokens (principal_id, id);
CREATE INDEX tokens_expiry ON tokens (expires_at) WHERE revoked_at IS NULL;
CREATE TABLE device_codes (
    device_code_hash bytea PRIMARY KEY CHECK (octet_length(device_code_hash) = 32),
    user_code_hash bytea NOT NULL UNIQUE CHECK (octet_length(user_code_hash) = 32),
    client_id text NOT NULL,
    scopes text[] NOT NULL DEFAULT '{}',
    audience text NOT NULL DEFAULT 'api' CHECK (audience IN ('api', 'mcp')),
    principal_id bigint REFERENCES principals,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    poll_interval_seconds integer NOT NULL DEFAULT 5 CHECK (poll_interval_seconds >= 5),
    last_polled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    CHECK (status NOT IN ('approved', 'consumed') OR principal_id IS NOT NULL)
);
CREATE INDEX device_codes_expiry ON device_codes (expires_at);

CREATE TABLE feeds (
    id bigserial PRIMARY KEY,
    url text NOT NULL CHECK (url ~ '^https?://'),
    owner_user_id bigint REFERENCES users,
    credentials_encrypted bytea,
    title text NOT NULL DEFAULT '',
    site_url text,
    language text NOT NULL DEFAULT 'simple',
    etag text,
    last_modified text,
    next_fetch_at timestamptz NOT NULL DEFAULT now(),
    fetch_interval interval NOT NULL DEFAULT interval '1 hour' CHECK (fetch_interval > interval '0'),
    backoff interval NOT NULL DEFAULT interval '0' CHECK (backoff >= interval '0'),
    last_success_at timestamptz,
    last_attempt_at timestamptz,
    error_class text CHECK (error_class IN ('dns', 'tls', 'network', 'timeout', 'gone', 'rate_limit', 'challenge', 'http', 'parse', 'size', 'policy', 'extraction')),
    error_message text,
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    last_http_status integer CHECK (last_http_status BETWEEN 100 AND 599),
    permanent_redirect_url text CHECK (permanent_redirect_url ~ '^https?://'),
    item_count bigint NOT NULL DEFAULT 0 CHECK (item_count >= 0),
    last_item_count integer NOT NULL DEFAULT 0 CHECK (last_item_count >= 0),
    extraction_mode text NOT NULL DEFAULT 'on_demand' CHECK (extraction_mode IN ('never', 'on_demand', 'always')),
    extraction_status text NOT NULL DEFAULT 'pending' CHECK (extraction_status IN ('pending', 'ok', 'failed', 'disabled')),
    extraction_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (credentials_encrypted IS NULL OR owner_user_id IS NOT NULL)
);
-- Public URLs deduplicate globally; private URLs deduplicate only within an owner.
CREATE UNIQUE INDEX feeds_public_url ON feeds (url) WHERE owner_user_id IS NULL;
CREATE UNIQUE INDEX feeds_private_url ON feeds (owner_user_id, url) WHERE owner_user_id IS NOT NULL;
CREATE INDEX feeds_due ON feeds (next_fetch_at, id);
CREATE TABLE folders (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    name text NOT NULL CHECK (btrim(name) <> ''),
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    deleted_at timestamptz,
    UNIQUE (user_id, id)
);
CREATE UNIQUE INDEX folders_name ON folders (user_id, name) WHERE deleted_at IS NULL;
CREATE TABLE subscriptions (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    feed_id bigint NOT NULL REFERENCES feeds,
    title_override text,
    folder_id bigint,
    cadence text NOT NULL DEFAULT 'today' CHECK (cadence IN ('now', 'today', 'week')),
    full_text_mode text NOT NULL DEFAULT 'inherit' CHECK (full_text_mode IN ('inherit', 'never', 'on_demand', 'always')),
    muted boolean NOT NULL DEFAULT false,
    mute_notifications boolean NOT NULL DEFAULT false,
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, folder_id) REFERENCES folders (user_id, id)
);
CREATE UNIQUE INDEX subscriptions_active ON subscriptions (user_id, feed_id) WHERE deleted_at IS NULL;
CREATE INDEX subscriptions_feed ON subscriptions (feed_id, user_id) WHERE deleted_at IS NULL;
CREATE INDEX subscriptions_bootstrap ON subscriptions (user_id, id);
CREATE INDEX subscriptions_folder ON subscriptions (user_id, folder_id, id) WHERE deleted_at IS NULL;
CREATE INDEX subscriptions_cadence ON subscriptions (user_id, cadence, id) WHERE deleted_at IS NULL AND NOT muted;
CREATE FUNCTION check_private_subscription() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_id bigint;
BEGIN
    SELECT owner_user_id INTO owner_id FROM feeds WHERE id = NEW.feed_id FOR SHARE;
    IF owner_id IS NOT NULL AND owner_id <> NEW.user_id THEN
        RAISE EXCEPTION 'private feed belongs to another user' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER subscriptions_private BEFORE INSERT OR UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION check_private_subscription();
CREATE FUNCTION keep_feed_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id THEN
        RAISE EXCEPTION 'feed ownership is immutable; create a separate feed' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER feeds_owner BEFORE UPDATE OF owner_user_id ON feeds FOR EACH ROW EXECUTE FUNCTION keep_feed_owner();

CREATE TABLE entries (
    id bigserial PRIMARY KEY,
    feed_id bigint NOT NULL REFERENCES feeds,
    seq bigint NOT NULL UNIQUE CHECK (seq > 0),
    identity_hash bytea NOT NULL CHECK (octet_length(identity_hash) = 32),
    guid text,
    url text,
    title text NOT NULL DEFAULT '',
    author text NOT NULL DEFAULT '',
    published_at timestamptz,
    updated_at timestamptz,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    content_hash bytea CHECK (octet_length(content_hash) = 32),
    language text NOT NULL DEFAULT 'simple',
    enclosures jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(enclosures) = 'array'),
    deleted_at timestamptz,
    search_vector tsvector NOT NULL DEFAULT '',
    UNIQUE (feed_id, identity_hash)
);
CREATE INDEX entries_river ON entries (feed_id, seq DESC) WHERE deleted_at IS NULL;
CREATE INDEX entries_retention ON entries (first_seen_at, seq) WHERE deleted_at IS NULL;
CREATE INDEX entries_url ON entries (url) WHERE deleted_at IS NULL;
CREATE INDEX entries_content_hash ON entries (content_hash) WHERE deleted_at IS NULL;
CREATE INDEX entries_search ON entries USING gin (search_vector);
-- Shared entries have a distinct change-log revision for every subscribing user.
CREATE TABLE entry_revisions (
    user_id bigint NOT NULL REFERENCES users,
    entry_id bigint NOT NULL REFERENCES entries,
    rev bigint NOT NULL CHECK (rev > 0),
    PRIMARY KEY (user_id, entry_id)
);
CREATE INDEX entry_revisions_entry ON entry_revisions (entry_id);
CREATE TABLE contents (
    entry_id bigint NOT NULL REFERENCES entries ON DELETE CASCADE,
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    sanitizer_version integer NOT NULL CHECK (sanitizer_version > 0),
    source text NOT NULL CHECK (source IN ('feed', 'extracted')),
    html text NOT NULL,
    text text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entry_id, content_hash, sanitizer_version, source)
);
-- Unknown language codes use simple. Explicit mapping prevents arbitrary regconfig input.
CREATE FUNCTION entry_search_config(language text) RETURNS regconfig LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE lower(split_part(replace(language, '_', '-'), '-', 1))
        WHEN 'en' THEN 'pg_catalog.english'::regconfig WHEN 'english' THEN 'pg_catalog.english'::regconfig
        WHEN 'de' THEN 'pg_catalog.german'::regconfig WHEN 'german' THEN 'pg_catalog.german'::regconfig
        WHEN 'fr' THEN 'pg_catalog.french'::regconfig WHEN 'french' THEN 'pg_catalog.french'::regconfig
        WHEN 'es' THEN 'pg_catalog.spanish'::regconfig WHEN 'spanish' THEN 'pg_catalog.spanish'::regconfig
        WHEN 'it' THEN 'pg_catalog.italian'::regconfig WHEN 'italian' THEN 'pg_catalog.italian'::regconfig
        WHEN 'pt' THEN 'pg_catalog.portuguese'::regconfig WHEN 'portuguese' THEN 'pg_catalog.portuguese'::regconfig
        WHEN 'nl' THEN 'pg_catalog.dutch'::regconfig WHEN 'dutch' THEN 'pg_catalog.dutch'::regconfig
        WHEN 'ru' THEN 'pg_catalog.russian'::regconfig WHEN 'russian' THEN 'pg_catalog.russian'::regconfig
        ELSE 'pg_catalog.simple'::regconfig END
$$;
CREATE FUNCTION update_entry_search() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE body text;
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        NEW.search_vector := ''::tsvector;
        RETURN NEW;
    END IF;
    SELECT c.text INTO body FROM contents c WHERE c.entry_id = NEW.id AND c.content_hash = NEW.content_hash
        ORDER BY (c.source = 'extracted') DESC, c.sanitizer_version DESC LIMIT 1;
    NEW.search_vector := setweight(to_tsvector(entry_search_config(NEW.language), NEW.title), 'A') ||
        setweight(to_tsvector(entry_search_config(NEW.language), coalesce(body, '')), 'B');
    RETURN NEW;
END $$;
CREATE TRIGGER entries_search BEFORE INSERT OR UPDATE OF title, language, content_hash, deleted_at ON entries
    FOR EACH ROW EXECUTE FUNCTION update_entry_search();
CREATE FUNCTION update_content_search() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE entries SET content_hash = content_hash WHERE id = OLD.entry_id;
        RETURN OLD;
    END IF;
    UPDATE entries SET content_hash = content_hash WHERE id = NEW.entry_id;
    RETURN NEW;
END $$;
CREATE TRIGGER contents_search AFTER INSERT OR UPDATE OR DELETE ON contents FOR EACH ROW EXECUTE FUNCTION update_content_search();

CREATE TABLE entry_state (
    principal_id bigint NOT NULL REFERENCES principals,
    entry_id bigint NOT NULL REFERENCES entries,
    seen text NOT NULL DEFAULT 'unseen' CHECK (seen IN ('seen', 'unseen')),
    seen_rev bigint NOT NULL DEFAULT 0 CHECK (seen_rev >= 0),
    seen_occurred_at timestamptz,
    seen_actor bigint REFERENCES principals,
    seen_source text CHECK (seen_source IN ('deliberate', 'rule', 'agent', 'passive')),
    shelf text NOT NULL DEFAULT 'none' CHECK (shelf IN ('none', 'later', 'done')),
    shelf_rev bigint NOT NULL DEFAULT 0 CHECK (shelf_rev >= 0),
    shelf_occurred_at timestamptz,
    shelf_actor bigint REFERENCES principals,
    shelf_source text CHECK (shelf_source IN ('deliberate', 'rule', 'agent', 'passive')),
    kept boolean NOT NULL DEFAULT false,
    kept_rev bigint NOT NULL DEFAULT 0 CHECK (kept_rev >= 0),
    kept_occurred_at timestamptz,
    kept_actor bigint REFERENCES principals,
    kept_source text CHECK (kept_source IN ('deliberate', 'rule', 'agent', 'passive')),
    state_rev bigint NOT NULL DEFAULT 0 CHECK (state_rev >= greatest(seen_rev, shelf_rev, kept_rev)),
    PRIMARY KEY (principal_id, entry_id),
    CHECK ((seen_rev = 0 AND seen = 'unseen' AND seen_occurred_at IS NULL AND seen_actor IS NULL AND seen_source IS NULL) OR
           (seen_rev > 0 AND seen_occurred_at IS NOT NULL AND seen_actor IS NOT NULL AND seen_source IS NOT NULL)),
    CHECK ((shelf_rev = 0 AND shelf = 'none' AND shelf_occurred_at IS NULL AND shelf_actor IS NULL AND shelf_source IS NULL) OR
           (shelf_rev > 0 AND shelf_occurred_at IS NOT NULL AND shelf_actor IS NOT NULL AND shelf_source IS NOT NULL)),
    CHECK ((kept_rev = 0 AND NOT kept AND kept_occurred_at IS NULL AND kept_actor IS NULL AND kept_source IS NULL) OR
           (kept_rev > 0 AND kept_occurred_at IS NOT NULL AND kept_actor IS NOT NULL AND kept_source IS NOT NULL))
);
CREATE INDEX entry_state_entry ON entry_state (entry_id);
CREATE INDEX entry_state_unread ON entry_state (principal_id, entry_id) WHERE seen = 'unseen' AND shelf <> 'done';
CREATE INDEX entry_state_shelf ON entry_state (principal_id, shelf, entry_id) WHERE shelf <> 'none';
CREATE INDEX entry_state_kept ON entry_state (principal_id, entry_id) WHERE kept;
CREATE TABLE positions (
    principal_id bigint NOT NULL REFERENCES principals,
    view text NOT NULL CHECK (view <> ''),
    anchor_seq bigint NOT NULL CHECK (anchor_seq >= 0),
    rev bigint NOT NULL CHECK (rev > 0),
    PRIMARY KEY (principal_id, view)
);
CREATE TABLE labels (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    name text NOT NULL CHECK (btrim(name) <> ''),
    rev bigint NOT NULL CHECK (rev > 0),
    deleted_at timestamptz,
    UNIQUE (user_id, id)
);
CREATE UNIQUE INDEX labels_name ON labels (user_id, name) WHERE deleted_at IS NULL;
CREATE TABLE entry_labels (
    user_id bigint NOT NULL REFERENCES users,
    entry_id bigint NOT NULL REFERENCES entries,
    label_id bigint NOT NULL,
    rev bigint NOT NULL CHECK (rev > 0),
    deleted_at timestamptz,
    PRIMARY KEY (user_id, entry_id, label_id),
    FOREIGN KEY (user_id, label_id) REFERENCES labels (user_id, id)
);
CREATE INDEX entry_labels_label ON entry_labels (user_id, label_id, entry_id) WHERE deleted_at IS NULL;
CREATE INDEX entry_labels_entry ON entry_labels (entry_id);
CREATE TABLE annotations (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    principal_id bigint NOT NULL,
    kind text NOT NULL CHECK (kind IN ('note', 'label', 'score', 'brief', 'summary', 'blurb')),
    value text NOT NULL,
    entry_ids bigint[] NOT NULL CHECK (cardinality(entry_ids) > 0 AND array_position(entry_ids, NULL) IS NULL),
    model text,
    prompt_version text,
    rev bigint NOT NULL CHECK (rev > 0),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, principal_id) REFERENCES principals (user_id, id)
);
CREATE INDEX annotations_bootstrap ON annotations (user_id, id);
CREATE INDEX annotations_principal ON annotations (principal_id, kind, id) WHERE deleted_at IS NULL;
CREATE INDEX annotations_entries ON annotations USING gin (entry_ids);
CREATE INDEX annotations_search ON annotations USING gin (to_tsvector('simple', value)) WHERE deleted_at IS NULL;

CREATE TABLE changes (
    user_id bigint NOT NULL REFERENCES users,
    rev bigint NOT NULL CHECK (rev > 0),
    type text NOT NULL CHECK (type <> ''),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, rev)
);
CREATE INDEX changes_retention ON changes (created_at);
CREATE TABLE op_ids (
    principal_id bigint NOT NULL REFERENCES principals,
    op_id text NOT NULL CHECK (op_id <> ''),
    result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, op_id)
);
CREATE INDEX op_ids_retention ON op_ids (created_at);
CREATE TABLE system (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    generation uuid NOT NULL DEFAULT gen_random_uuid()
);
INSERT INTO system DEFAULT VALUES;
CREATE TABLE seq_counter (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    value bigint NOT NULL DEFAULT 0 CHECK (value >= 0)
);
INSERT INTO seq_counter DEFAULT VALUES;
CREATE TABLE rev_counters (
    user_id bigint PRIMARY KEY REFERENCES users,
    value bigint NOT NULL DEFAULT 0 CHECK (value >= 0)
);

CREATE TABLE notifications (
    id bigserial PRIMARY KEY,
    sender_principal_id bigint NOT NULL REFERENCES principals,
    recipient_user_id bigint NOT NULL REFERENCES users,
    op_id text NOT NULL CHECK (op_id <> ''),
    token_id bigint REFERENCES tokens,
    note text NOT NULL CHECK (length(note) <= 8192),
    entry_ids bigint[] NOT NULL CHECK (cardinality(entry_ids) > 0 AND array_position(entry_ids, NULL) IS NULL),
    category text NOT NULL CHECK (category IN ('alert', 'brief', 'digest', 'edition', 'watch')),
    dedupe_key text,
    status text NOT NULL CHECK (status IN ('held', 'queued', 'attempting', 'delivered', 'uncertain', 'cancelled', 'withheld', 'failed')),
    held_until timestamptz,
    generation uuid NOT NULL,
    attempted_at timestamptz,
    delivered_at timestamptz,
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (sender_principal_id, op_id),
    CHECK (status <> 'held' OR held_until IS NOT NULL),
    CHECK (status NOT IN ('attempting', 'delivered', 'uncertain') OR attempted_at IS NOT NULL),
    CHECK (status <> 'delivered' OR delivered_at IS NOT NULL),
    CHECK (delivered_at IS NULL OR attempted_at IS NOT NULL)
);
CREATE INDEX notifications_recipient ON notifications (recipient_user_id, id DESC);
CREATE INDEX notifications_due ON notifications (status, held_until, id) WHERE status IN ('held', 'queued');
CREATE INDEX notifications_dedupe ON notifications (sender_principal_id, dedupe_key, created_at DESC) WHERE dedupe_key IS NOT NULL;
CREATE INDEX notifications_budget ON notifications (token_id, created_at) WHERE token_id IS NOT NULL;
CREATE TABLE devices (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    transport text NOT NULL CHECK (transport IN ('ntfy', 'webpush', 'unifiedpush', 'apns')),
    endpoint text NOT NULL CHECK (endpoint <> ''),
    keys jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(keys) = 'object'),
    show_text boolean NOT NULL DEFAULT false,
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, transport, endpoint)
);
CREATE TABLE push_settings (
    user_id bigint PRIMARY KEY REFERENCES users,
    ntfy_server text,
    ntfy_topic text,
    ntfy_token_encrypted bytea,
    quiet_start time,
    quiet_end time,
    timezone text NOT NULL DEFAULT 'UTC',
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    CHECK ((quiet_start IS NULL) = (quiet_end IS NULL))
);
CREATE TABLE activity (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    principal_id bigint NOT NULL,
    action text NOT NULL,
    target jsonb NOT NULL,
    before jsonb,
    after jsonb,
    revert_state jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, principal_id) REFERENCES principals (user_id, id)
);
CREATE INDEX activity_user ON activity (user_id, id DESC);
CREATE INDEX activity_principal ON activity (principal_id, id DESC);
CREATE FUNCTION immutable_activity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'activity is append-only' USING ERRCODE = '23514';
END $$;
CREATE TRIGGER activity_immutable BEFORE UPDATE OR DELETE ON activity FOR EACH ROW EXECUTE FUNCTION immutable_activity();
CREATE TABLE webhooks (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    created_by bigint NOT NULL,
    url text NOT NULL CHECK (url ~ '^https?://'),
    secret_encrypted bytea NOT NULL,
    events text[] NOT NULL CHECK (cardinality(events) > 0),
    active boolean NOT NULL DEFAULT true,
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, created_by) REFERENCES principals (user_id, id)
);
CREATE INDEX webhooks_user ON webhooks (user_id, id);
CREATE FUNCTION human_webhook() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM principals WHERE id = NEW.created_by AND kind = 'human') THEN
        RAISE EXCEPTION 'webhooks must be created by a human' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER webhooks_human BEFORE INSERT OR UPDATE ON webhooks FOR EACH ROW EXECUTE FUNCTION human_webhook();
CREATE TABLE saved_searches (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    name text NOT NULL CHECK (name <> ''),
    query text NOT NULL,
    filters jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(filters) = 'object'),
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    deleted_at timestamptz,
    UNIQUE (user_id, name)
);
CREATE TABLE rules (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    source text NOT NULL,
    ast jsonb NOT NULL CHECK (jsonb_typeof(ast) = 'object'),
    action jsonb NOT NULL CHECK (jsonb_typeof(action) = 'object'),
    enabled boolean NOT NULL DEFAULT true,
    created_by bigint NOT NULL,
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, created_by) REFERENCES principals (user_id, id)
);
CREATE INDEX rules_enabled ON rules (user_id, id) WHERE enabled;
CREATE TABLE mutes (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    kind text NOT NULL CHECK (kind IN ('keyword', 'author')),
    value text NOT NULL CHECK (value <> ''),
    rev bigint NOT NULL DEFAULT 0 CHECK (rev >= 0),
    deleted_at timestamptz,
    UNIQUE (user_id, kind, value)
);
CREATE TABLE clusters (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    title text NOT NULL DEFAULT '',
    rev bigint NOT NULL CHECK (rev > 0),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id)
);
CREATE TABLE entry_clusters (
    user_id bigint NOT NULL REFERENCES users,
    cluster_id bigint NOT NULL,
    entry_id bigint NOT NULL REFERENCES entries,
    rev bigint NOT NULL CHECK (rev > 0),
    deleted_at timestamptz,
    PRIMARY KEY (user_id, cluster_id, entry_id),
    FOREIGN KEY (user_id, cluster_id) REFERENCES clusters (user_id, id)
);
CREATE INDEX entry_clusters_entry ON entry_clusters (entry_id, user_id);
CREATE TABLE embeddings (
    entry_id bigint NOT NULL REFERENCES entries ON DELETE CASCADE,
    model text NOT NULL CHECK (model <> ''),
    dims integer NOT NULL CHECK (dims BETWEEN 1 AND 1024),
    embedding halfvec(1024) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entry_id, model),
    CHECK (CASE WHEN dims < 1024 THEN l2_norm(subvector(embedding, dims + 1, 1024 - dims)) = 0 ELSE true END)
);
CREATE INDEX embeddings_cosine ON embeddings USING hnsw (embedding halfvec_cosine_ops);
CREATE TABLE llm_cache (
    owner_user_id bigint REFERENCES users,
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    kind text NOT NULL CHECK (kind IN ('label', 'score', 'brief', 'summary', 'blurb', 'edition')),
    prompt_version text NOT NULL,
    model text NOT NULL,
    output text NOT NULL,
    citations jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(citations) = 'array'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (owner_user_id, content_hash, kind, prompt_version, model)
);
CREATE TABLE llm_spend (
    provider text NOT NULL CHECK (provider <> ''),
    month date NOT NULL CHECK (extract(day FROM month) = 1),
    spent_usd numeric(14,6) NOT NULL DEFAULT 0 CHECK (spent_usd >= 0),
    limit_usd numeric(14,6) NOT NULL CHECK (limit_usd >= 0),
    PRIMARY KEY (provider, month)
);
CREATE TABLE jobs (
    id bigserial PRIMARY KEY,
    kind text NOT NULL CHECK (kind <> ''),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    run_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text,
    locked_until timestamptz,
    unique_key text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX jobs_due ON jobs (run_at, id);
CREATE INDEX jobs_leases ON jobs (locked_until) WHERE locked_until IS NOT NULL;
