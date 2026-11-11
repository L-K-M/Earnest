ALTER TABLE folders ADD COLUMN parent_id bigint;
ALTER TABLE folders ADD CONSTRAINT folders_parent_owner
    FOREIGN KEY (user_id, parent_id) REFERENCES folders (user_id, id);
ALTER TABLE folders ADD CONSTRAINT folders_not_own_parent
    CHECK (parent_id IS NULL OR parent_id <> id);
CREATE INDEX folders_parent ON folders (user_id, parent_id) WHERE deleted_at IS NULL;
