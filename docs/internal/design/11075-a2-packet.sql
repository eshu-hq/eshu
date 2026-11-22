-- #11075 AC3 A2-identical 60-min read-only evidence packet.
--
-- Refs #11075. Part of #10860. Parent: #10839 (Gate B leg-(b)).
--
-- Run ONLY after the deployed QA commit includes ae142dcb77 (#11086).
-- SELECT-only against the QA read replica (service eshu-postgresql-read,
-- namespace eshu). Set a statement timeout and a read-only transaction
-- before every pull; write nothing. 31 pulls at exact 120 s cadence over
-- 60 min; record launch skew per pull. Thresholds are judged on the full
-- trailing 60 min only. A failed packet keeps #11075 open with the failing
-- bars named: no threshold edits, no third sampling lane under new bars.
--
-- Connection (read-only role; never the primary):
--   kubectl -n eshu port-forward svc/eshu-postgresql-read 54399:5432
--   psql "$READ_DSN"   -- host rewritten to 127.0.0.1:54399
--   PGOPTIONS="-c statement_timeout=30000 -c default_transaction_read_only=on"
--
-- R(t) is the handler's own watermark: the singleton row read by
-- FactStore.AdvisoryGroupWinnersWatermark
-- (go/internal/storage/postgres/supply_chain_impact_advisory_group_winners.go).
--
-- Definitions (arbiter ruling 2026-11-22, #11075 gap rulings; no bar changed):
--   * First classification: a row whose failure details carried no
--     `winners-watermark-deferral:` marker when the handler evaluated it. After
--     that evaluation the row carries a marker whose first-deferred-at is that
--     evaluation's time. The reliable test is set-based: the work_item_id is
--     new in the Q4b snapshot of this pull. Q4 is the per-pull proxy.
--     `attempt_count = 1` is NOT a filter for first evaluations: a not_ready
--     park freezes attempt_count at 1, so re-evaluations also read 1.
--   * Numerator for pre-claim lag: the marker's own clock,
--     first-deferred-at minus cycle-started-at, the handler's measure. The
--     COALESCE(reopened_at, created_at) figure is reported beside it, labelled
--     `legacy`, and the two are never mixed in one number.
--   * Resolutions and satisfaction come from set differences of the Q4b id
--     snapshots (Q6). Satisfied means the id left the winners class and its
--     row now carries no failure.
-- Q0-Q3 and Q5 were validated live against the QA replica 2026-11-21. Q4, Q4b
-- and Q6 were rewritten 2026-11-22 and have NOT been executed against
-- Postgres yet: dry-run each once on the replica before pull 1.

-- Q0. Replica liveness (header of every pull).
SELECT now() AS ts,
       pg_is_in_recovery() AS in_recovery,
       now() - pg_last_xact_replay_timestamp() AS replay_lag;

-- Q1. Deploy precondition: the #11086 marker must be present on parked rows.
-- Near-zero share means QA still runs pre-fix code: do not judge AC3 bars.
SELECT now() AS ts,
       count(*) FILTER (
           WHERE failure_details::text LIKE '%winners-watermark-deferral:%'
       ) AS with_marker,
       count(*) AS watermark_wait_rows
  FROM fact_work_items
 WHERE status IN ('retrying', 'claimed')
   AND failure_class LIKE '%winners_watermark%'
   AND attempt_count >= 2;

-- Q2. Backlog state at pull time.
SELECT now() AS ts, status, failure_class, count(*)
  FROM fact_work_items
 WHERE failure_class LIKE '%winners_watermark%'
 GROUP BY 1, 2, 3 ORDER BY 2, 3;

SELECT now() AS ts, waits_for_kind, count(*) AS n,
       min(first_deferred_at) AS oldest_anchor,
       max(first_deferred_at) AS newest_anchor
  FROM reducer_waiting_items
 GROUP BY 1, 2 ORDER BY 2;

SELECT now() AS ts, stage, count(*) AS advisory_pending,
       avg(now() - created_at) AS mean_age,
       max(now() - created_at) AS max_age
  FROM fact_work_items
 WHERE domain = 'advisory_group_projection' AND status = 'pending'
 GROUP BY 1, 2;

-- Q3. R(t): the watermark value and wall-R lag (paired spans judge pace).
SELECT now() AS ts, singleton, materialized_at AS r,
       now() - materialized_at AS wall_r_lag
  FROM supply_chain_impact_winners_materialization;

-- Q4. First classifications in the trailing pull interval (per-pull proxy).
-- A first classification is a row whose marker first-deferred-at falls in the
-- interval and within 120 s of last_attempt_at (the evaluation that wrote the
-- marker). Re-evaluations keep the older first-deferred-at and drop out.
-- The packet judges first classifications from the Q4b set difference; this
-- query supplies the per-row lag and verdict class for each pull.
-- Numerator: marker clock. The legacy column is reported beside it.
-- The timestamp regexes match RFC3339Nano as the handler writes it, so a
-- malformed marker reads as NULL and drops out instead of failing the cast.
WITH wl AS (
    SELECT work_item_id, failure_class, status, last_attempt_at,
           COALESCE(reopened_at, created_at) AS legacy_cycle_start,
           substring(failure_details FROM
               'first-deferred-at=([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z)')
               ::timestamptz AS marker_first_deferred_at,
           substring(failure_details FROM
               'cycle-started-at=([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z)')
               ::timestamptz AS marker_cycle_started_at
      FROM fact_work_items
     WHERE domain = 'advisory_group_projection'
       AND failure_class LIKE '%winners_watermark%'
)
SELECT now() AS ts, failure_class, status,
       count(*) AS n,
       max(marker_first_deferred_at - marker_cycle_started_at)
           AS max_pre_claim_lag_marker,
       max(last_attempt_at - legacy_cycle_start)
           AS max_pre_claim_lag_legacy
  FROM wl
 WHERE marker_first_deferred_at > now() - interval '120 seconds'
   AND abs(extract(epoch FROM (last_attempt_at - marker_first_deferred_at)))
       <= 120
 GROUP BY 1, 2, 3;

-- Q4b. Work-item id snapshot, saved on every pull. Resolutions, first
-- classifications and satisfaction are set differences of these snapshots, so
-- launch skew cannot leave holes the way summed 120 s slices can.
SELECT now() AS ts, work_item_id, failure_class, status, attempt_count,
       last_attempt_at, reopened_at, created_at,
       substring(failure_details FROM
           'first-deferred-at=([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z)')
           AS marker_first_deferred_at,
       substring(failure_details FROM
           'cycle-started-at=([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z)')
           AS marker_cycle_started_at
  FROM fact_work_items
 WHERE domain = 'advisory_group_projection'
   AND failure_class LIKE '%winners_watermark%';

-- Q5. Inflow split (prefix method until #11078 lands its tally instrument;
-- name the method in the packet).
SELECT now() AS ts, 'staging' AS rel,
       count(*) FILTER (WHERE advisory_id LIKE 'GHSA-%') AS ghsa,
       count(*) FILTER (WHERE advisory_id LIKE 'CVE-%') AS cve,
       count(*) FILTER (WHERE advisory_id NOT LIKE 'GHSA-%'
                        AND advisory_id NOT LIKE 'CVE-%') AS other
  FROM supply_chain_impact_winners_dirty_staging
UNION ALL
SELECT now(), 'log',
       count(*) FILTER (WHERE advisory_id LIKE 'GHSA-%'),
       count(*) FILTER (WHERE advisory_id LIKE 'CVE-%'),
       count(*) FILTER (WHERE advisory_id NOT LIKE 'GHSA-%'
                        AND advisory_id NOT LIKE 'CVE-%')
  FROM supply_chain_impact_winners_dirty_log;

SELECT now() AS ts, count(*) AS tally_rows
  FROM supply_chain_impact_winners_dirty_tally;

-- Q6. Resolutions and satisfaction (offline set differences plus one lookup).
-- From the saved Q4b files, for each pair of consecutive pulls k-1 and k:
--   resolved(k)  = ids in snapshot k-1 and not in snapshot k (left the class)
--   first(k)     = ids in snapshot k and not in any earlier snapshot
-- Snapshot 1 is the baseline: its ids are not first classifications.
-- Sum over the 31 pulls for the closing table. For every resolved id, look up
-- where the row went. Pass the ids with:  \set left_ids '{id1,id2,...}'
-- (\set is interpolated only from psql stdin, -f or an interactive session,
-- not from -c).
SELECT now() AS ts, work_item_id, status, failure_class
  FROM fact_work_items
 WHERE work_item_id = ANY(:'left_ids'::text[]);
-- An id in `left_ids` with no row here, or with status 'succeeded', is
-- SATISFIED (left the class without a failure row). An id
-- now under another failure class, or in dead_letter outside the winners
-- class, is resolved but not satisfied. Report both counts. The packet needs
-- at least 1 satisfied row.

-- Q7. Closing evaluation (after pull 31, over the saved 31 pull outputs):
--   first-claim lag max < 30 min (max over first classifications of the Q4
--     marker numerator; report the legacy column beside it, labelled);
--   first-classification stale count = 0 (rows first seen in a Q4b snapshot
--     with failure_class `..._stale`; a re-evaluation is not a first stale);
--   resolutions >= first-classifications (Q6 resolved total >= Q4b first total);
--   retrying + dead_letter stocks non-rising (Q2 first pull vs last pull);
--   wall-R lag closed or shrinking on paired spans with dR/dwall > 1 (Q3);
--   >= 1 satisfied row (Q6); sustained inflow with the split named (Q5).
