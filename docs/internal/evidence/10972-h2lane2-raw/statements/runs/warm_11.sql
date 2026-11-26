-- DIAGNOSTIC WARMUP ONLY (labeled exception, read-only): same text, raised guard.
BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '120s';
SET LOCAL enable_seqscan = 'off';
\timing on
PREPARE lane2_warm(text[]) AS SELECT ids.id AS lookup_id, LEAST(COALESCE(cve.mass, 0) + COALESCE(adv.mass, 0) + COALESCE(m3.mass, 0), 78436) AS mass
FROM UNNEST($1::text[]) AS ids(id)
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > 78436 THEN 78436 ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.cve_id <> ''
          AND support.cve_id = ids.id
        LIMIT 78437
    ) AS limited
) AS cve ON TRUE
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > 78436 THEN 78436 ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.advisory_id <> ''
          AND support.advisory_id = ids.id
        LIMIT 78437
    ) AS limited
) AS adv ON TRUE
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > 100 THEN 78436 ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.payload #> '{provenance,advisory_group_members}' ? ids.id
        LIMIT 101
    ) AS limited
) AS m3 ON (COALESCE(cve.mass, 0) + COALESCE(adv.mass, 0)) < 78436;
EXECUTE lane2_warm(ARRAY['GHSA-pmwg-cvhr-8vh7','GHSA-q2hr-2g5m-vwhr','GHSA-qhr7-859c-m2p7','GHSA-qx2v-qp2m-jg93','GHSA-r3ph-w7gj-g6xm']::text[]);
DEALLOCATE lane2_warm;
COMMIT;
