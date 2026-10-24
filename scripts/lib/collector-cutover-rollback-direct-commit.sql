-- eshu-rollback:direct-commit
-- Read-only. The "in-tree producer resumed" signal for a DIRECT in-tree
-- collector, which runs as its own Deployment with no collector instance and
-- writes no workflow_work_items rows (#9027).
-- Output: generations|latest_activated_at
-- Variables: kind, since (database timestamp).
--
-- The signal is the number of generations of this collector kind that were
-- observed at or after the window start (ingested_at) and have been projected
-- (activated_at set, status active or superseded), plus the newest activation
-- time (empty when none). Both timestamps are needed. A collector such as
-- Confluence emits its generation as pending, and the projector stamps
-- activated_at only when it acks the generation. A generation the external
-- producer observed before the window and the projector acked after it has an
-- old ingested_at, so it does not count. Rows still pending, failed, or never
-- activated do not count either.
--
-- Clock note: ingested_at is the collector pod's clock and since is the database
-- clock. A collector clock behind the database clock by more than the poll delay
-- only times the gate out, which is safe. One ahead by more than the gap between
-- the external stop and the window start could pass early; that gap is minutes.
--
-- A source that did not change mints no new generation (the generation ID is a
-- content hash and a published generation is never reopened), so this signal
-- stays at zero until the source changes: the gate times out rather than report
-- a resume it did not see. The read scans the small ingestion_scopes set (there
-- is no collector_kind index) and probes scope_generations_scope_generation_idx
-- for each scope; it runs once per poll, not on a hot path.
SELECT
    count(*),
    COALESCE(to_char(max(generation.activated_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE scope.collector_kind = :'kind'
  AND generation.status IN ('active', 'superseded')
  AND generation.ingested_at >= :'since'::timestamptz
  AND generation.activated_at >= :'since'::timestamptz;
