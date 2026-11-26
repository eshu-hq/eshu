-- H2 census step 1: fast table stats (10s guard).
SET statement_timeout = '10s';
SELECT reltuples::bigint AS est_rows,
  pg_size_pretty(pg_total_relation_size('supply_chain_impact_finding_supports')) AS total,
  pg_size_pretty(pg_relation_size('supply_chain_impact_finding_supports')) AS heap
  FROM pg_class WHERE relname = 'supply_chain_impact_finding_supports';
SELECT current_setting('shared_buffers') AS shared_buffers;
