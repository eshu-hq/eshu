-- 332_supply_chain_impact_winners_payload_backfill.sql: fill the
-- payload and source_confidence migration 331 added, for the winner
-- rows that already exist when it lands (#10880 slice 1 follow-up).
--
-- 331 alone leaves every pre-existing winner at payload '{}' and
-- source_confidence 'unknown', and the slice-2 list read serves
-- win.payload directly (no fact_records payload join remains), so an
-- upgraded deployment would serve empty payloads between deploy and
-- the first post-deploy resweep. This backfill makes the columns
-- true at migration time instead of at first-resweep time.
--
-- The payload expression replicates the winner select's bake
-- (supply_chain_impact_canonical_winners_store.go): the winning
-- fact's payload normalized by stripping the volatile
-- digest-excluded keys (intent_id, cause, generation_id, and
-- provenance.content_digest; #10880 arbiter ruling 2026-11-21),
-- with the authoritative operator suppression object merged in
-- when an active operator-scope finding overrides the same
-- canonical key. The operator override is picked with the
-- winner select's own rule: active operator findings with a
-- non-active suppression_state, ranked by priority_score DESC, then
-- a payload-carried finding_id, then fact_id.
--
-- Idempotent: the UPDATE only touches rows still at the 331 default
-- '{}' (a real winner payload always carries finding fields, never
-- an empty object), so a replay of this file -- an untracked
-- database's one full replay, or a retried interrupted apply --
-- fills nothing twice, and a fresh install backfills nothing. A
-- winner whose fact row is already gone keeps '{}'; the maintainer
-- invariant says that cannot happen (winners are reconciled to
-- active facts), and the next resweep would delete or rewrite such
-- a row either way.
--
-- Locking: this is a file of its own, so the runner's single
-- simple-query string is this one statement in its own implicit
-- transaction, instead of extending 331's ACCESS EXCLUSIVE ALTER --
-- which blocks every reader of the table -- across the backfill
-- scan (the migration 126/127 split, #7961). It takes ROW EXCLUSIVE
-- on supply_chain_impact_canonical_winners -- readers are not
-- blocked -- and a row lock on each winner row it fills, probing
-- fact_records by primary key (winner_fact_id) once per winner; the
-- operator CTE reads only the operator scope's active generation,
-- bounded by the operators' suppressions. Duration is linear in
-- the winners row count, the same shape as one full resweep, which
-- already runs on a cadence. A resweep racing this statement on the
-- same winner row waits on that row lock, bounded by the runner's
-- lock_timeout and retry.
WITH active_operator AS (
    SELECT
        fact.fact_id,
        fact.payload,
        CONCAT_WS('|',
            COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
            COALESCE(fact.payload->>'advisory_id', ''),
            COALESCE(fact.payload->>'package_id', ''),
            COALESCE(fact.payload->>'purl', ''),
            COALESCE(fact.payload->>'product_criteria', ''),
            COALESCE(fact.payload->>'match_criteria_id', ''),
            COALESCE(fact.payload->>'observed_version', ''),
            COALESCE(fact.payload->>'requested_range', ''),
            COALESCE(fact.payload->>'impact_status', ''),
            COALESCE(fact.payload->>'repository_id', ''),
            COALESCE(fact.payload->>'subject_digest', '')
        ) AS canonical_key,
        COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
        CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END AS has_payload_finding_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.scope_id = 'operator:vulnerability_suppressions'
      AND COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'
),
operator_overrides AS (
    SELECT DISTINCT ON (canonical_key) *
    FROM active_operator
    ORDER BY canonical_key,
             priority_score DESC,
             has_payload_finding_id DESC,
             fact_id ASC
),
fills AS (
    SELECT w.canonical_key,
           CASE
               WHEN override.fact_id IS NULL THEN ((winner.payload - 'intent_id' - 'cause' - 'generation_id') #- '{provenance,content_digest}')::jsonb
               ELSE ((winner.payload - 'intent_id' - 'cause' - 'generation_id') #- '{provenance,content_digest}') || jsonb_build_object('suppression', COALESCE(CASE WHEN jsonb_typeof(override.payload->'suppression') = 'object' THEN override.payload->'suppression' END, '{}'::jsonb))
           END AS payload,
           winner.source_confidence
    FROM supply_chain_impact_canonical_winners AS w
    JOIN fact_records AS winner
      ON winner.fact_id = w.winner_fact_id
    LEFT JOIN operator_overrides AS override
      ON override.canonical_key = w.canonical_key
    WHERE w.payload = '{}'::jsonb
)
UPDATE supply_chain_impact_canonical_winners AS w
SET payload = fills.payload,
    source_confidence = fills.source_confidence
FROM fills
WHERE fills.canonical_key = w.canonical_key
  AND w.payload = '{}'::jsonb;
