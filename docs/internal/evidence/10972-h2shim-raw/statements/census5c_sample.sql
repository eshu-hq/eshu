-- H2 census step 5c: bounded long-member probe on a 1% sample.
-- Full unnest detoasts 13M payloads (ground 30m+ on the throttled primary,
-- killed); this sample bounds the cost. Diagnostic 600s timeout, read-only.
SET statement_timeout = '600s';
BEGIN READ ONLY;
SELECT m AS long_member, octet_length(m) AS bytes
  FROM supply_chain_impact_finding_supports TABLESAMPLE BERNOULLI(1),
  LATERAL (SELECT jsonb_array_elements_text(payload #> '{provenance,advisory_group_members}') AS m) t
  WHERE octet_length(m) > 1024 LIMIT 5;
COMMIT;
