-- One server-wide configuration. The authenticated envelope includes API keys;
-- no plaintext provider configuration is written to user settings or change logs.
CREATE TABLE IF NOT EXISTS llm_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0),
    config_encrypted bytea NOT NULL CHECK (octet_length(config_encrypted) BETWEEN 28 AND 1048576)
);
