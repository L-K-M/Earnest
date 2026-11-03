-- Bind pending jobs from before generation tracking once, at upgrade time.
-- Explicit generations, including stale ones restored from backups, stay intact.
UPDATE jobs
SET payload = payload || jsonb_build_object('generation', (SELECT generation::text FROM system WHERE singleton))
WHERE kind = 'rule' AND NOT (payload ? 'generation');
