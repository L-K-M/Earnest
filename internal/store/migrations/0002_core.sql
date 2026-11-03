ALTER TABLE feeds ADD COLUMN lease_until timestamptz;
ALTER TABLE feeds ADD COLUMN posts_per_day double precision NOT NULL DEFAULT 0 CHECK (posts_per_day >= 0);
ALTER TABLE subscriptions ADD COLUMN initial_since timestamptz NOT NULL DEFAULT '-infinity';
ALTER TABLE rev_counters ADD COLUMN retention_floor bigint NOT NULL DEFAULT 0 CHECK (retention_floor >= 0 AND retention_floor <= value);
-- The model deliberately combines rules and agents into one provenance class.
ALTER TABLE entry_state DROP CONSTRAINT entry_state_seen_source_check;
ALTER TABLE entry_state DROP CONSTRAINT entry_state_shelf_source_check;
ALTER TABLE entry_state DROP CONSTRAINT entry_state_kept_source_check;
ALTER TABLE entry_state ADD CHECK (seen_source IN ('deliberate','rule','agent','rule_or_agent','passive'));
ALTER TABLE entry_state ADD CHECK (shelf_source IN ('deliberate','rule','agent','rule_or_agent','passive'));
ALTER TABLE entry_state ADD CHECK (kept_source IN ('deliberate','rule','agent','rule_or_agent','passive'));
CREATE INDEX feeds_lease ON feeds (lease_until) WHERE lease_until IS NOT NULL;
-- Membership identity must not collapse different entries carrying one label.
ALTER TABLE entry_labels ADD COLUMN id bigserial UNIQUE NOT NULL;
-- Only the first fetched cohort is subject to the subscription import cutoff.
ALTER TABLE subscriptions ADD COLUMN initial_through_seq bigint NOT NULL DEFAULT 0 CHECK (initial_through_seq >= 0);
ALTER TABLE subscriptions ADD COLUMN initial_fetch_pending boolean NOT NULL DEFAULT false;
