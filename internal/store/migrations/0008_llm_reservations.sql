-- Estimates count against spent_usd until settled. Expiration retains the
-- estimate: a crashed caller may have been billed before it lost the response.
CREATE TABLE llm_spend_reservations (
 id text PRIMARY KEY CHECK (id <> ''),
 provider text NOT NULL,
 month date NOT NULL,
 estimate_micros bigint NOT NULL CHECK (estimate_micros >= 0),
 actual_micros bigint CHECK (actual_micros >= 0),
 expires_at timestamptz NOT NULL,
 expired_at timestamptz,
 settled_at timestamptz,
 FOREIGN KEY (provider, month) REFERENCES llm_spend (provider, month),
 CHECK ((actual_micros IS NULL) = (settled_at IS NULL))
);
CREATE INDEX llm_reservations_pending ON llm_spend_reservations (provider, expires_at)
 WHERE settled_at IS NULL AND expired_at IS NULL;
