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
-- Cycle start is COALESCE(reopened_at, created_at): never created_at alone,
-- because a reopen resets the deferral clock. Pre-claim lag for an
-- attempt-1 row is last_attempt_at - cycle_start.
-- Every query below was validated live against the QA replica 2026-11-21.

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

-- Q4. First evaluations in the trailing pull interval (per-row verdicts).
-- attempt_count = 1 rows whose first evaluation fell in the interval:
-- every one must be not_ready regardless of pre-claim lag (zero stale).
SELECT now() AS ts, failure_class,
       count(*) AS n,
       max(last_attempt_at - COALESCE(reopened_at, created_at))
           AS max_pre_claim_lag
  FROM fact_work_items
 WHERE domain = 'advisory_group_projection'
   AND failure_class LIKE '%winners_watermark%'
   AND attempt_count = 1
   AND last_attempt_at > now() - interval '120 seconds'
 GROUP BY 1, 2;

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

-- Q6. Satisfaction leg: rows the watermark satisfied in the interval
-- (E-R <= 0 read per row is the handler's verdict; here count advisory
-- rows that left the watermark wait by projection in the interval).
-- Concrete form depends on the projection write path at run time; the
-- packet must name the query used and show >= 1 satisfied row, else the
-- packet fails per the Q1-only refusal.

-- Q7. Closing evaluation (after pull 31, over the saved 31 pull outputs):
--   first-claim lag max < 30 min (max of Q4 max_pre_claim_lag);
--   attempt-1 stale count = 0 (Q4 stale rows = 0 on all pulls);
--   resolutions >= first-classifications (Q6 total >= Q4 total);
--   retrying + dead_letter stocks non-rising (Q2 first pull vs last pull);
--   wall-R lag closed or shrinking on paired spans with dR/dwall > 1 (Q3);
--   >= 1 satisfied row (Q6); sustained inflow with the split named (Q5).
