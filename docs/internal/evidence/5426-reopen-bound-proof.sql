-- Theory proof for the per-drain correlation-reopen bound (PR #5846 follow-up).
--
-- THEORY: RunDeferredRelationshipMaintenance runs on EVERY shard drain, and
-- succeeded reducer rows are never terminalized, so an unbounded corpus-wide
-- replay grows linearly with generation count. A per-scope REPLAY FLOOR — keep
-- only the work items on the scope's active generation or newer, falling back
-- to the scope's LATEST generation when there is no usable active generation —
-- bounds the per-drain replay to O(active scopes).
--
-- The floor replaced an earlier "exclude rows whose scope has a strictly newer
-- ACTIVE generation" predicate. That predicate carried an
-- `active_generation.generation_id IS NOT NULL` guard, which reads a scope whose
-- ACTIVE generation FAILED (failProjectorWorkQuery nulls active_generation_id)
-- as "never activated" and therefore reopens EVERY generation of that scope on
-- EVERY drain, forever. Shape 2 below measures that hole; shape 3 closes it.
--
-- Representative worst case, not the average: a long-lived store where every
-- scope has been re-ingested many times, plus the three "no usable active
-- generation" scopes that reach this listing in production.
--
-- Run:
--   psql "$DSN" -v ON_ERROR_STOP=1 -f docs/internal/evidence/5426-reopen-bound-proof.sql

DROP SCHEMA IF EXISTS reopen_bound_proof CASCADE;
CREATE SCHEMA reopen_bound_proof;
SET search_path TO reopen_bound_proof;

CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT NULL
);

-- Production has no foreign key on active_generation_id, only this partial
-- unique index (schema/data-plane/postgres/001_ingestion_scopes.sql), which is
-- why the pointer can dangle at a generation row that retention removed.
CREATE UNIQUE INDEX ingestion_scopes_active_generation_idx
    ON ingestion_scopes (active_generation_id)
    WHERE active_generation_id IS NOT NULL;

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    ingested_at TIMESTAMPTZ NOT NULL,
    -- NOT NULL in schema/data-plane/postgres/002_scope_generations.sql. The
    -- default only keeps the bulk seeds below three-column.
    status TEXT NOT NULL DEFAULT 'pending'
);

-- Verbatim from schema/data-plane/postgres/002_scope_generations.sql.
CREATE INDEX scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);

CREATE TABLE fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    domain TEXT NOT NULL,
    status TEXT NOT NULL,
    visible_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Verbatim from schema/data-plane/postgres/005_fact_work_items.sql. The
-- visible_at column between status and updated_at matters: it is what stops the
-- index from serving the listing's ORDER BY without a sort, and an earlier
-- version of this proof measured a 4-column index production does not have.
CREATE INDEX fact_work_items_stage_domain_status_idx
    ON fact_work_items (stage, domain, status, visible_at, updated_at DESC);

CREATE INDEX fact_work_items_scope_generation_idx
    ON fact_work_items (scope_id, generation_id, status, updated_at DESC);

-- 900 scopes x 25 generations = 22 500 succeeded supply_chain_impact rows,
-- 900 of which sit on the scope's current active generation.
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT 'scope-' || s, NULL FROM generate_series(1, 900) AS s;

INSERT INTO scope_generations (generation_id, scope_id, ingested_at)
SELECT 'gen-' || s || '-' || g,
       'scope-' || s,
       TIMESTAMPTZ '2026-01-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, 900) AS s, generate_series(1, 25) AS g;

UPDATE ingestion_scopes
SET active_generation_id = 'gen-' || split_part(scope_id, '-', 2) || '-25';

INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, updated_at)
SELECT 'wi-' || s || '-' || g,
       'scope-' || s,
       'gen-' || s || '-' || g,
       'reducer',
       'supply_chain_impact',
       'succeeded',
       NULL,
       TIMESTAMPTZ '2026-01-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, 900) AS s, generate_series(1, 25) AS g;

-- Scope shape A: never activated. This is the activation race the replay exists
-- for, so its row MUST survive every bound.
INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ('scope-unactivated', NULL);
INSERT INTO scope_generations (generation_id, scope_id, ingested_at)
VALUES ('gen-unactivated-1', 'scope-unactivated', TIMESTAMPTZ '2026-01-01 00:00:00+00');
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, updated_at)
VALUES ('wi-unactivated', 'scope-unactivated', 'gen-unactivated-1',
        'reducer', 'supply_chain_impact', 'succeeded', NULL, TIMESTAMPTZ '2026-01-01 00:00:00+00');

-- Scope shape B: the ACTIVE generation failed, so failProjectorWorkQuery set its
-- status to 'failed' and nulled active_generation_id in the same statement. 25
-- generations, all with a succeeded correlation row. Under the IS NOT NULL guard
-- all 25 reopen on every drain; under the floor alone, 1 — the failed generation
-- itself, which no query can read and which therefore re-succeeds and reopens
-- forever. Under the shipped shape (floor + failed exclusion), 0.
INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ('scope-failed', NULL);
INSERT INTO scope_generations (generation_id, scope_id, ingested_at)
SELECT 'gen-failed-' || g, 'scope-failed',
       TIMESTAMPTZ '2026-01-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, 25) AS g;
UPDATE scope_generations SET status = 'failed' WHERE generation_id = 'gen-failed-25';
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, updated_at)
SELECT 'wi-failed-' || g, 'scope-failed', 'gen-failed-' || g,
       'reducer', 'supply_chain_impact', 'succeeded', NULL,
       TIMESTAMPTZ '2026-01-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, 25) AS g;

-- Scope shape C: active_generation_id dangles at a generation row that is gone.
-- No foreign key prevents this, and it has the same effect as shape B.
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
VALUES ('scope-dangling', 'gen-deleted-by-retention');
INSERT INTO scope_generations (generation_id, scope_id, ingested_at)
SELECT 'gen-dangling-' || g, 'scope-dangling',
       TIMESTAMPTZ '2026-01-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, 25) AS g;
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, updated_at)
SELECT 'wi-dangling-' || g, 'scope-dangling', 'gen-dangling-' || g,
       'reducer', 'supply_chain_impact', 'succeeded', NULL,
       TIMESTAMPTZ '2026-01-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, 25) AS g;

ANALYZE;

\echo '=== Shape 1 OLD (unbounded: one listing per drain over all history) ==='
EXPLAIN (ANALYZE, BUFFERS, TIMING)
SELECT work_item_id
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = 'supply_chain_impact'
  AND status = 'succeeded'
ORDER BY updated_at ASC, work_item_id ASC;

\echo '=== Shape 2 GUARD-ONLY (superseded exclusion with IS NOT NULL guard) ==='
EXPLAIN (ANALYZE, BUFFERS, TIMING)
SELECT work.work_item_id
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
JOIN scope_generations AS stale_generation
  ON stale_generation.scope_id = work.scope_id
 AND stale_generation.generation_id = work.generation_id
LEFT JOIN scope_generations AS active_generation
  ON active_generation.scope_id = work.scope_id
 AND active_generation.generation_id = scope.active_generation_id
WHERE work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status = 'succeeded'
  AND NOT (
    active_generation.generation_id IS NOT NULL
    AND work.generation_id <> scope.active_generation_id
    AND (
      stale_generation.ingested_at < active_generation.ingested_at
      OR (
        stale_generation.ingested_at = active_generation.ingested_at
        AND stale_generation.generation_id < active_generation.generation_id
      )
    )
  )
ORDER BY work.updated_at ASC, work.work_item_id ASC;

\echo '=== Shape 3 FLOOR-ONLY (per-scope replay floor, MATERIALIZED) ==='
EXPLAIN (ANALYZE, BUFFERS, TIMING)
WITH scope_replay_floor AS MATERIALIZED (
  SELECT scope.scope_id,
         COALESCE(active_generation.ingested_at, latest_generation.ingested_at) AS floor_ingested_at,
         COALESCE(active_generation.generation_id, latest_generation.generation_id) AS floor_generation_id
  FROM ingestion_scopes AS scope
  LEFT JOIN scope_generations AS active_generation
    ON active_generation.scope_id = scope.scope_id
   AND active_generation.generation_id = scope.active_generation_id
  LEFT JOIN LATERAL (
      SELECT candidate.ingested_at, candidate.generation_id
      FROM scope_generations AS candidate
      WHERE candidate.scope_id = scope.scope_id
      ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
      LIMIT 1
  ) AS latest_generation ON true
)
SELECT work.work_item_id
FROM fact_work_items AS work
JOIN scope_generations AS work_generation
  ON work_generation.scope_id = work.scope_id
 AND work_generation.generation_id = work.generation_id
JOIN scope_replay_floor AS floor
  ON floor.scope_id = work.scope_id
WHERE work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status = 'succeeded'
  AND (work_generation.ingested_at, work_generation.generation_id)
      >= (floor.floor_ingested_at, floor.floor_generation_id)
ORDER BY work.updated_at ASC, work.work_item_id ASC;

\echo '=== Shape 4 SHIPPED (replay floor + failed-generation exclusion) ==='
EXPLAIN (ANALYZE, BUFFERS, TIMING)
WITH scope_replay_floor AS MATERIALIZED (
  SELECT scope.scope_id,
         COALESCE(active_generation.ingested_at, latest_generation.ingested_at) AS floor_ingested_at,
         COALESCE(active_generation.generation_id, latest_generation.generation_id) AS floor_generation_id
  FROM ingestion_scopes AS scope
  LEFT JOIN scope_generations AS active_generation
    ON active_generation.scope_id = scope.scope_id
   AND active_generation.generation_id = scope.active_generation_id
  LEFT JOIN LATERAL (
      SELECT candidate.ingested_at, candidate.generation_id
      FROM scope_generations AS candidate
      WHERE candidate.scope_id = scope.scope_id
      ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
      LIMIT 1
  ) AS latest_generation ON true
)
SELECT work.work_item_id
FROM fact_work_items AS work
JOIN scope_generations AS work_generation
  ON work_generation.scope_id = work.scope_id
 AND work_generation.generation_id = work.generation_id
JOIN scope_replay_floor AS floor
  ON floor.scope_id = work.scope_id
WHERE work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status = 'succeeded'
  AND work_generation.status <> 'failed'
  AND (work_generation.ingested_at, work_generation.generation_id)
      >= (floor.floor_ingested_at, floor.floor_generation_id)
ORDER BY work.updated_at ASC, work.work_item_id ASC;

\echo '=== Shape 3b FLOOR-ONLY without the MATERIALIZED hint (why the hint is there) ==='
EXPLAIN (ANALYZE, BUFFERS, TIMING)
WITH scope_replay_floor AS NOT MATERIALIZED (
  SELECT scope.scope_id,
         COALESCE(active_generation.ingested_at, latest_generation.ingested_at) AS floor_ingested_at,
         COALESCE(active_generation.generation_id, latest_generation.generation_id) AS floor_generation_id
  FROM ingestion_scopes AS scope
  LEFT JOIN scope_generations AS active_generation
    ON active_generation.scope_id = scope.scope_id
   AND active_generation.generation_id = scope.active_generation_id
  LEFT JOIN LATERAL (
      SELECT candidate.ingested_at, candidate.generation_id
      FROM scope_generations AS candidate
      WHERE candidate.scope_id = scope.scope_id
      ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
      LIMIT 1
  ) AS latest_generation ON true
)
SELECT work.work_item_id
FROM fact_work_items AS work
JOIN scope_generations AS work_generation
  ON work_generation.scope_id = work.scope_id
 AND work_generation.generation_id = work.generation_id
JOIN scope_replay_floor AS floor
  ON floor.scope_id = work.scope_id
WHERE work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status = 'succeeded'
  AND (work_generation.ingested_at, work_generation.generation_id)
      >= (floor.floor_ingested_at, floor.floor_generation_id)
ORDER BY work.updated_at ASC, work.work_item_id ASC;

\echo '=== Rows reopened per drain: OLD vs GUARD-ONLY vs FLOOR vs SHIPPED ==='
CREATE VIEW shape_old AS
  SELECT work_item_id, scope_id, generation_id
  FROM fact_work_items
  WHERE stage = 'reducer' AND domain = 'supply_chain_impact' AND status = 'succeeded';

CREATE VIEW shape_guard AS
  SELECT work.work_item_id, work.scope_id, work.generation_id
  FROM fact_work_items AS work
  JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
  JOIN scope_generations AS stale_generation
    ON stale_generation.scope_id = work.scope_id
   AND stale_generation.generation_id = work.generation_id
  LEFT JOIN scope_generations AS active_generation
    ON active_generation.scope_id = work.scope_id
   AND active_generation.generation_id = scope.active_generation_id
  WHERE work.stage = 'reducer' AND work.domain = 'supply_chain_impact' AND work.status = 'succeeded'
    AND NOT (
      active_generation.generation_id IS NOT NULL
      AND work.generation_id <> scope.active_generation_id
      AND (
        stale_generation.ingested_at < active_generation.ingested_at
        OR (stale_generation.ingested_at = active_generation.ingested_at
            AND stale_generation.generation_id < active_generation.generation_id)
      )
    );

CREATE VIEW shape_floor AS
  WITH scope_replay_floor AS MATERIALIZED (
    SELECT scope.scope_id,
           COALESCE(active_generation.ingested_at, latest_generation.ingested_at) AS floor_ingested_at,
           COALESCE(active_generation.generation_id, latest_generation.generation_id) AS floor_generation_id
    FROM ingestion_scopes AS scope
    LEFT JOIN scope_generations AS active_generation
      ON active_generation.scope_id = scope.scope_id
     AND active_generation.generation_id = scope.active_generation_id
    LEFT JOIN LATERAL (
        SELECT candidate.ingested_at, candidate.generation_id
        FROM scope_generations AS candidate
        WHERE candidate.scope_id = scope.scope_id
        ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
        LIMIT 1
    ) AS latest_generation ON true
  )
  SELECT work.work_item_id, work.scope_id, work.generation_id
  FROM fact_work_items AS work
  JOIN scope_generations AS work_generation
    ON work_generation.scope_id = work.scope_id
   AND work_generation.generation_id = work.generation_id
  JOIN scope_replay_floor AS floor
    ON floor.scope_id = work.scope_id
  WHERE work.stage = 'reducer' AND work.domain = 'supply_chain_impact' AND work.status = 'succeeded'
    AND (work_generation.ingested_at, work_generation.generation_id)
        >= (floor.floor_ingested_at, floor.floor_generation_id);

-- The shipped shape: the floor, plus the exclusion of work items whose OWN
-- generation is terminally failed. The exclusion is on the work item's
-- generation rather than on the floor's fallback candidate set because lowering
-- the floor to the newest non-failed generation would keep the failed
-- generation's rows (still at or above the lowered floor) churning and start the
-- older generation's rows churning too — see the per-scope table below.
--
-- This view must express the SHIPPED predicate, not an equivalent rewrite of it.
-- It therefore reuses Shape 4's exact construction: the same inner join onto
-- scope_generations on BOTH (scope_id, generation_id), carrying the same
-- work_generation.status <> 'failed'. An earlier revision filtered with
-- `generation_id NOT IN (SELECT generation_id FROM scope_generations WHERE
-- status = 'failed')`, which happens to agree here only because generation_id is
-- the primary key and so is never NULL — a nullable column would make NOT IN
-- return zero rows silently — and which could drift from the shipped predicate
-- independently.
CREATE VIEW shape_shipped AS
  SELECT floor_rows.work_item_id, floor_rows.scope_id, floor_rows.generation_id
  FROM shape_floor AS floor_rows
  JOIN scope_generations AS work_generation
    ON work_generation.scope_id = floor_rows.scope_id
   AND work_generation.generation_id = floor_rows.generation_id
  WHERE work_generation.status <> 'failed';

SELECT (SELECT count(*) FROM shape_old)     AS old_rows,
       (SELECT count(*) FROM shape_guard)   AS guard_rows,
       (SELECT count(*) FROM shape_floor)   AS floor_rows,
       (SELECT count(*) FROM shape_shipped) AS shipped_rows;

\echo '=== The per-drain hole each bound leaves open (P1-2, then PR #5850 P2) ==='
SELECT scope_id,
       (SELECT count(*) FROM shape_guard g WHERE g.scope_id = s.scope_id)   AS guard_rows_per_drain,
       (SELECT count(*) FROM shape_floor f WHERE f.scope_id = s.scope_id)   AS floor_rows_per_drain,
       (SELECT count(*) FROM shape_shipped h WHERE h.scope_id = s.scope_id) AS shipped_rows_per_drain
FROM (VALUES ('scope-failed'), ('scope-dangling'), ('scope-unactivated'), ('scope-1')) AS s(scope_id)
ORDER BY scope_id;

\echo '=== Expected delta: SHIPPED adds nothing, drops only unreadable generations, keeps the race ==='
SELECT
  (SELECT count(*) FROM (SELECT work_item_id FROM shape_shipped
                         EXCEPT SELECT work_item_id FROM shape_old) AS x)
    AS shipped_not_in_old_must_be_0,
  (SELECT count(*) FROM (SELECT work_item_id FROM shape_old
                         EXCEPT SELECT work_item_id FROM shape_shipped) AS y)
    AS dropped_by_shipped,
  (SELECT count(*) FROM shape_shipped f
     JOIN ingestion_scopes s ON s.scope_id = f.scope_id
    WHERE s.active_generation_id IS NOT NULL
      AND EXISTS (SELECT 1 FROM scope_generations a
                   WHERE a.generation_id = s.active_generation_id)
      AND f.generation_id <> s.active_generation_id)
    AS kept_but_superseded_must_be_0,
  (SELECT count(*) FROM shape_shipped s
     JOIN scope_generations g ON g.generation_id = s.generation_id
    WHERE g.status = 'failed')
    AS kept_but_failed_must_be_0,
  (SELECT count(*) FROM shape_shipped WHERE work_item_id = 'wi-unactivated')
    AS unactivated_kept_must_be_1;

DROP SCHEMA reopen_bound_proof CASCADE;
