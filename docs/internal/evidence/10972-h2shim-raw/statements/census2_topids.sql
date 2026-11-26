-- H2 census step 2: top advisory / CVE ids by true COUNT(*).
-- Diagnostic-only raised timeout (one census pass, read-only transactions).
-- 300s proved too short on the throttled primary; 1800s for these two scans.
SET statement_timeout = '1800s';
BEGIN READ ONLY;
SELECT advisory_id, COUNT(*) AS rows FROM supply_chain_impact_finding_supports
  WHERE advisory_id <> '' GROUP BY 1 ORDER BY 2 DESC LIMIT 8;
COMMIT;
BEGIN READ ONLY;
SELECT cve_id, COUNT(*) AS rows FROM supply_chain_impact_finding_supports
  WHERE cve_id <> '' GROUP BY 1 ORDER BY 2 DESC LIMIT 8;
COMMIT;
