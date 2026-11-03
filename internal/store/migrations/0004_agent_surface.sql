ALTER TABLE push_settings ADD COLUMN show_text boolean NOT NULL DEFAULT false;
ALTER TABLE webhooks ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0);
ALTER TABLE notifications ADD COLUMN urgent boolean NOT NULL DEFAULT false;
ALTER TABLE notifications ADD COLUMN why jsonb NOT NULL DEFAULT '{}';
ALTER TABLE notifications ADD COLUMN budget_used integer NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN budget_limit integer NOT NULL DEFAULT 0;
ALTER TABLE system ADD COLUMN notifications_released boolean NOT NULL DEFAULT true;
CREATE FUNCTION withhold_generation_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.generation <> OLD.generation THEN
        NEW.notifications_released := false;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER system_delivery_generation BEFORE UPDATE OF generation ON system
    FOR EACH ROW EXECUTE FUNCTION withhold_generation_delivery();
CREATE TABLE notification_mutes (
    user_id bigint NOT NULL,
    principal_id bigint NOT NULL,
    PRIMARY KEY (user_id, principal_id),
    FOREIGN KEY (user_id, principal_id) REFERENCES principals(user_id, id)
);
CREATE FUNCTION activity_current_generation() RETURNS uuid LANGUAGE sql STABLE AS $$
    SELECT generation FROM system WHERE singleton
$$;
ALTER TABLE activity ADD COLUMN generation uuid NOT NULL DEFAULT activity_current_generation();
CREATE TABLE activity_reverts (
    activity_id bigint PRIMARY KEY REFERENCES activity,
    reverted_by bigint NOT NULL REFERENCES principals,
    created_at timestamptz NOT NULL
);
CREATE TABLE triage_handles (
    handle_hash bytea PRIMARY KEY CHECK (octet_length(handle_hash)=32),
    principal_id bigint NOT NULL REFERENCES principals,
    generation uuid NOT NULL,
    activity_ids bigint[] NOT NULL,
    expires_at timestamptz NOT NULL,
    op_id text NOT NULL,
    UNIQUE (principal_id, op_id)
);
-- Cover non-human own-state writes, which the existing human-triage logger excludes.
CREATE FUNCTION activity_state_snapshot(s entry_state) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
 SELECT jsonb_build_object('principal',s.principal_id,'entry_id',s.entry_id,
 'seen',jsonb_build_object('value',coalesce(s.seen,'unseen'),'rev',coalesce(s.seen_rev,0),'occurred_at',s.seen_occurred_at,'actor',s.seen_actor,'source',s.seen_source),
 'shelf',jsonb_build_object('value',coalesce(s.shelf,'none'),'rev',coalesce(s.shelf_rev,0),'occurred_at',s.shelf_occurred_at,'actor',s.shelf_actor,'source',s.shelf_source),
 'kept',jsonb_build_object('value',coalesce(s.kept,false),'rev',coalesce(s.kept_rev,0),'occurred_at',s.kept_occurred_at,'actor',s.kept_actor,'source',s.kept_source),
 'state_rev',coalesce(s.state_rev,0))
$$;
CREATE FUNCTION log_nonhuman_own_state() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor_id bigint; actor_kind text; owner_id bigint; previous jsonb;
BEGIN
    IF NEW.seen_rev IS DISTINCT FROM OLD.seen_rev AND NEW.seen_rev > 0 THEN actor_id := NEW.seen_actor;
    ELSIF NEW.shelf_rev IS DISTINCT FROM OLD.shelf_rev AND NEW.shelf_rev > 0 THEN actor_id := NEW.shelf_actor;
    ELSIF NEW.kept_rev IS DISTINCT FROM OLD.kept_rev AND NEW.kept_rev > 0 THEN actor_id := NEW.kept_actor;
    ELSE RETURN NEW;
    END IF;
    SELECT kind,user_id INTO actor_kind,owner_id FROM principals WHERE id=actor_id;
    IF actor_kind='pipeline' OR (actor_kind='agent' AND actor_id=NEW.principal_id) THEN
        previous := activity_state_snapshot(OLD) || jsonb_build_object('principal',NEW.principal_id,'entry_id',NEW.entry_id);
        INSERT INTO activity(user_id,principal_id,action,target,before,after,created_at)
        VALUES(owner_id,actor_id,'state.updated',jsonb_build_object('principal',NEW.principal_id,'entry_id',NEW.entry_id),
          previous,activity_state_snapshot(NEW),now());
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER entry_state_activity AFTER INSERT OR UPDATE ON entry_state
    FOR EACH ROW EXECUTE FUNCTION log_nonhuman_own_state();
CREATE FUNCTION log_nonhuman_position() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_id bigint; actor_kind text; actor_id bigint;
BEGIN
    actor_id := coalesce(nullif(current_setting('earnest.position_actor',true),'')::bigint,NEW.principal_id);
    SELECT user_id,kind INTO owner_id,actor_kind FROM principals WHERE id=actor_id;
    IF actor_kind <> 'human' THEN
        INSERT INTO activity(user_id,principal_id,action,target,before,after)
        VALUES(owner_id,actor_id,'position.updated',
          jsonb_build_object('principal',NEW.principal_id,'view',NEW.view),
          to_jsonb(OLD),to_jsonb(NEW));
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER positions_activity AFTER INSERT OR UPDATE ON positions
    FOR EACH ROW EXECUTE FUNCTION log_nonhuman_position();
