-- #10880 PR-B1: the scale generator ties multi-support keys to non-eligible statuses,
-- so it cannot show per-support precise-eligibility divergence. This patch makes every
-- even multi-support key affected_exact on ALL of its supports (the key includes
-- impact_status, so supports of one key must agree) and rebuilds those supports rows.
\set ON_ERROR_STOP on
BEGIN;
UPDATE fact_records
SET payload = payload || '{"impact_status":"affected_exact"}'::jsonb
WHERE fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact_id ~ '(^|:)fact:\d+:\d$'
  AND (substring(fact_id from 'fact:(\d+):'))::int % 10 IN (7, 8, 9)
  AND (substring(fact_id from 'fact:(\d+):'))::int % 2 = 0;
DELETE FROM supply_chain_impact_finding_supports
WHERE fact_id ~ '(^|:)fact:\d+:\d$'
  AND (substring(fact_id from 'fact:(\d+):'))::int % 10 IN (7, 8, 9)
  AND (substring(fact_id from 'fact:(\d+):'))::int % 2 = 0;
COMMIT;
