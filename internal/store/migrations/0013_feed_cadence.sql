-- Keep publisher cadence separate from retry/poll intervals and HTTP caching.
-- The watermark prevents unchanged feed documents from reweighting a sample.
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS posting_interval interval
    CHECK (posting_interval >= interval '1 second' AND posting_interval <= interval '365 days');
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS posting_sample_at timestamptz;

-- Reconsider previously stored healthy cache waits under the new policy.
-- Do not shorten error backoff or Retry-After, or disturb an active lease.
UPDATE feeds SET next_fetch_at = LEAST(next_fetch_at, now() + interval '1 hour')
WHERE consecutive_failures = 0 AND last_attempt_at IS NOT NULL;
