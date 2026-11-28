-- SPDX-License-Identifier: MIT
-- #9209 C3 acceptance: at least 95% of CVE-keyed findings carry EPSS.
--
-- Refs eshu-hq/eshu#9209. Arbiter ruling (2026-11-28, #11613): C3 denominator
-- is CVE-keyed findings (option a). CVE-keyed means active winners whose
-- keying cve_id matches the findings-derived EPSS pattern
-- `(?i)^CVE-[0-9]{4}-[0-9]{4,}$` (epss_finding_derivation.go:35,
-- case-insensitive; stored ids are uppercase, so ~ and ~* agree).
-- Numerator is EPSS score present in the winners payload
-- (`epss_probability` non-empty; top-level `epss_percentile` is equivalent
-- on this corpus).
--
-- Run ONLY read-only against a standby replica. This file opens and rolls
-- back one READ ONLY transaction; it writes nothing and holds no locks.
-- Connection (the QA read replica; never the primary):
--   kubectl --context <qa-context> -n eshu exec -i eshu-postgresql-read-0 \
--     -c postgresql
--   psql -U eshu -d eshu
--   \i docs/internal/design/9209-c3-acceptance.sql
--
-- Definitions (shared by Q1-Q3 below):
--   * Active/effective-visible: effective suppression state outside the
--     hidden set (`not_affected`, `accepted_risk`, `false_positive`,
--     `ignored`, `advisory_retracted` from
--     go/internal/reducer/supplychain/core/evaluation.go:83-91), with the
--     expiry-aware CASE of advisoryGroupWinnerEffectiveStateSQL
--     (supply_chain_impact_advisory_group_winners.go:56-66): an expired
--     suppression reads 'expired' and stays visible. Ported verbatim here
--     because winners columns are already TIMESTAMPTZ.
--   * CVE-keyed: cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$' (see above).
--   * Row-weighted: one count per canonical-winners row (no DISTINCT),
--     matching the epic QA check "EPSS on at least 95% of CVE-keyed
--     findings" (advisory-intelligence-phases.md:131).
--   * Ceiling: payload-present plus stale (fact exists, payload missing);
--     equals the fact-join rate while the FIRST catalog misses hold.
--
-- Q1: canonical CVE-keyed row-weighted payload query (the C3 decision
-- number), with all-active and ceiling beside it.
-- Q2: CVE-keyed fact-join variant (existence of an active
-- vulnerability.epss_score fact for the winner's cve_id).
-- Q3: breakdowns: ecosystems, per-CVE missing classes, GHSA/GO counts,
-- distinct-CVE counts, pin.
BEGIN READ ONLY;
SET LOCAL statement_timeout = '120000';

-- Q1 ---------------------------------------------------------------------
WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
)
SELECT 'Q1_c3_payload' AS query,
  count(*) FILTER (WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$') AS cvekeyed_denom,
  count(*) FILTER (WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
    AND COALESCE(payload->>'epss_probability','') <> '') AS cvekeyed_present,
  round(100.0 * count(*) FILTER (WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
    AND COALESCE(payload->>'epss_probability','') <> '')
    / nullif(count(*) FILTER (WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'),0), 3) AS cvekeyed_pct,
  count(*) AS allactive_denom,
  count(*) FILTER (WHERE COALESCE(payload->>'epss_probability','') <> '') AS allactive_present,
  round(100.0 * count(*) FILTER (WHERE COALESCE(payload->>'epss_probability','') <> '')
    / nullif(count(*),0), 3) AS allactive_pct,
  count(*) FILTER (WHERE COALESCE(payload->>'epss_probability','') = '') AS allactive_missing,
  count(*) FILTER (WHERE cve_id = '') AS blank_cve_id
FROM vis;

-- Q2 ---------------------------------------------------------------------
WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
),
eps_active AS (
  SELECT DISTINCT payload->>'cve_id' AS cve
  FROM fact_records
  WHERE fact_kind = 'vulnerability.epss_score' AND is_tombstone = false
),
cvekeyed AS (
  SELECT v.*, (e.cve IS NOT NULL) AS has_eps_fact
  FROM vis v LEFT JOIN eps_active e ON e.cve = v.cve_id
  WHERE v.cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
)
SELECT 'Q2_c3_factjoin' AS query,
  count(*) AS cvekeyed_denom,
  count(*) FILTER (WHERE has_eps_fact) AS factjoin_present,
  round(100.0 * count(*) FILTER (WHERE has_eps_fact) / nullif(count(*),0), 3) AS factjoin_pct,
  count(*) FILTER (WHERE COALESCE(payload->>'epss_probability','') = '' AND has_eps_fact) AS stale_rows_fact_exists_payload_missing,
  count(*) FILTER (WHERE COALESCE(payload->>'epss_probability','') = '' AND NOT has_eps_fact) AS catalogmiss_rows_no_fact,
  round(100.0 * count(*) FILTER (WHERE COALESCE(payload->>'epss_probability','') <> '' OR has_eps_fact)
    / nullif(count(*),0), 3) AS ceiling_pct_cvekeyed
FROM cvekeyed;

-- Q2b: all-active ceiling (payload present plus stale, over all-active).
WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
),
eps_active AS (
  SELECT DISTINCT payload->>'cve_id' AS cve
  FROM fact_records
  WHERE fact_kind = 'vulnerability.epss_score' AND is_tombstone = false
)
SELECT 'Q2b_ceiling_allactive' AS query,
  count(*) AS allactive_denom,
  count(*) FILTER (WHERE COALESCE(v.payload->>'epss_probability','') <> '' OR e.cve IS NOT NULL) AS ceiling_numerator,
  round(100.0 * count(*) FILTER (WHERE COALESCE(v.payload->>'epss_probability','') <> '' OR e.cve IS NOT NULL)
    / nullif(count(*),0), 3) AS ceiling_pct,
  ceil(0.95 * count(*)) AS rows_needed_for_95,
  (ceil(0.95 * count(*))
    - count(*) FILTER (WHERE COALESCE(v.payload->>'epss_probability','') <> '' OR e.cve IS NOT NULL)) AS ceiling_shortfall_to_95
FROM vis v LEFT JOIN eps_active e ON e.cve = v.cve_id;

-- Q3: breakdowns (one statement each; each is independently re-runnable).
WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
)
SELECT 'Q3a_eco' AS query, ecosystem,
  count(*) FILTER (WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$') AS cvekeyed_rows,
  count(*) FILTER (WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
    AND COALESCE(payload->>'epss_probability','') = '') AS cvekeyed_missing_payload,
  count(*) AS total_rows
FROM vis GROUP BY ecosystem ORDER BY total_rows DESC;

WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
),
missing AS (
  SELECT cve_id, count(*) AS rows_cnt FROM vis
  WHERE cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
    AND COALESCE(payload->>'epss_probability','') = ''
  GROUP BY cve_id
),
eps AS (
  SELECT DISTINCT payload->>'cve_id' AS cve
  FROM fact_records
  WHERE fact_kind = 'vulnerability.epss_score' AND is_tombstone = false
)
SELECT 'Q3b_missing_by_cve' AS query, m.cve_id, m.rows_cnt,
  CASE WHEN e.cve IS NOT NULL THEN 'has_fact_stale' ELSE 'no_fact_catalog' END AS class
FROM missing m LEFT JOIN eps e ON e.cve = m.cve_id
ORDER BY m.rows_cnt DESC, m.cve_id;

WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
)
SELECT 'Q3c_noncve' AS query,
  count(*) FILTER (WHERE cve_id LIKE 'GHSA-%') AS ghsa_rows,
  count(DISTINCT cve_id) FILTER (WHERE cve_id LIKE 'GHSA-%') AS ghsa_distinct,
  count(*) FILTER (WHERE cve_id LIKE 'GO-%') AS go_rows,
  count(DISTINCT cve_id) FILTER (WHERE cve_id LIKE 'GO-%') AS go_distinct,
  count(*) FILTER (WHERE cve_id <> '' AND cve_id !~* '^CVE-[0-9]{4}-[0-9]{4,}$'
    AND cve_id NOT LIKE 'GHSA-%' AND cve_id NOT LIKE 'GO-%') AS other_noncve_rows,
  count(*) FILTER (WHERE cve_id LIKE 'GHSA-%' AND COALESCE(payload->>'epss_probability','') <> '') AS ghsa_payload_present,
  count(*) FILTER (WHERE cve_id LIKE 'GO-%' AND COALESCE(payload->>'epss_probability','') <> '') AS go_payload_present
FROM vis;

WITH eff AS (
  SELECT w.*,
    CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END AS eff_state
  FROM supply_chain_impact_canonical_winners w
),
vis AS (
  SELECT * FROM eff
  WHERE eff_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
),
eps_active AS (
  SELECT DISTINCT payload->>'cve_id' AS cve
  FROM fact_records
  WHERE fact_kind = 'vulnerability.epss_score' AND is_tombstone = false
),
cvekeyed AS (
  SELECT v.*, (e.cve IS NOT NULL) AS has_eps_fact
  FROM vis v LEFT JOIN eps_active e ON e.cve = v.cve_id
  WHERE v.cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
)
SELECT 'Q3d_distinct' AS query,
  count(DISTINCT cve_id) AS distinct_denom,
  count(DISTINCT cve_id) FILTER (WHERE COALESCE(payload->>'epss_probability','') <> '') AS distinct_payload_present,
  count(DISTINCT cve_id) FILTER (WHERE has_eps_fact) AS distinct_factjoin_present,
  round(100.0 * count(DISTINCT cve_id) FILTER (WHERE COALESCE(payload->>'epss_probability','') <> '')
    / nullif(count(DISTINCT cve_id),0), 3) AS distinct_payload_pct,
  round(100.0 * count(DISTINCT cve_id) FILTER (WHERE has_eps_fact)
    / nullif(count(DISTINCT cve_id),0), 3) AS distinct_factjoin_pct
FROM cvekeyed;

-- Q3e: pin (transaction wall time, recovery, materialization watermark).
SELECT 'Q3e_pin' AS query,
  now() AS transaction_now,
  pg_is_in_recovery() AS in_recovery,
  (SELECT max(materialized_at) FROM supply_chain_impact_canonical_winners) AS winners_watermark_max,
  (SELECT count(*) FROM supply_chain_impact_canonical_winners) AS winners_rows,
  (SELECT count(*) FROM fact_records WHERE fact_kind = 'vulnerability.epss_score') AS epss_score_facts,
  (SELECT count(DISTINCT payload->>'cve_id') FROM fact_records WHERE fact_kind = 'vulnerability.epss_score') AS epss_distinct_cves;

ROLLBACK;
