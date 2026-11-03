-- Bind pending jobs from before generation tracking once, at upgrade time.
-- Explicit generations, including stale ones restored from backups, stay intact.
-- A closed restore gate leaves legacy jobs unbound so release cannot replay them.
UPDATE jobs
SET payload = payload || jsonb_build_object('generation', (SELECT generation::text FROM system WHERE singleton))
WHERE kind = 'rule' AND NOT (payload ? 'generation')
    AND (SELECT notifications_released FROM system WHERE singleton);
