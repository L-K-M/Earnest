ALTER TABLE users ADD COLUMN compat_password_hash text CHECK (compat_password_hash IS NULL OR compat_password_hash LIKE '$argon2id$%');
