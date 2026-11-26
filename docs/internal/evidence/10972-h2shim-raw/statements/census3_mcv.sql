-- H2 census step 3: instant planner stats for candidate small ids (10s guard).
SET statement_timeout = '10s';
SELECT attname, null_frac, n_distinct, most_common_vals::text AS mcv
  FROM pg_stats WHERE tablename = 'supply_chain_impact_finding_supports'
  AND attname IN ('advisory_id', 'cve_id');
