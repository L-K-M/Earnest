ALTER TABLE folders ADD COLUMN IF NOT EXISTS parent_id bigint
    CONSTRAINT folders_not_own_parent CHECK (parent_id <> id);
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'folders'::regclass AND conname = 'folders_parent_owner') THEN
        ALTER TABLE folders ADD CONSTRAINT folders_parent_owner
            FOREIGN KEY (user_id, parent_id) REFERENCES folders (user_id, id);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS folders_parent ON folders (user_id, parent_id) WHERE deleted_at IS NULL;
