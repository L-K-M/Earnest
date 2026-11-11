-- One server-wide configuration. The authenticated envelope includes API keys;
-- no plaintext provider configuration is written to user settings or change logs.
CREATE TABLE IF NOT EXISTS llm_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0),
    config_encrypted bytea NOT NULL CHECK (octet_length(config_encrypted) BETWEEN 28 AND 1048576)
);

-- A pending provider batch keeps its original routing, credentials and prices
-- across Settings edits and restarts. The worker deletes unreferenced snapshots.
CREATE TABLE IF NOT EXISTS llm_batch_configs (
    config_hash text PRIMARY KEY CHECK (config_hash ~ '^[0-9a-f]{64}$'),
    config_encrypted bytea NOT NULL CHECK (octet_length(config_encrypted) BETWEEN 28 AND 1048576),
    legacy boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX IF NOT EXISTS llm_batch_configs_legacy ON llm_batch_configs(legacy) WHERE legacy;
