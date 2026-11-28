# Holder key-range gate retune

Performance Evidence (gate 78435 to 3000): executing `EXPLAIN (ANALYZE,
BUFFERS)` on the QA read replica at QA mass (155 GB supports table) showed
shipped singletons in the 9k–78k row middle band breaching the 2s
per-statement bar — a 29,417-row id ran the shipped holder statement in
2.36s partly-cold — while the same id reads as 16 key-range statements in
0.56s total at 0.105s max per statement with a byte-identical 110-holder
set. The gate is the largest comfortably-passing mass halved (6006/2,
rounded to 3000); ids at or under it keep the cheap shipped statement and
everything hotter ranges. Full numbers, rejected alternatives, and proof
limits: retired evidence record.

Observability Evidence (gate 78435 to 3000): no new metric or span. The
existing per-pass `fanout_holder_statements` and `fanout_max_id_mass_rows`
sub-signals plus `eshu_dp_supply_chain_impact_fanout_site_duration_seconds`
(site `holder_reads`) expose the routing outcome; rollout watch is
production p95/max against the 2s bar and `supply_chain_impact_fanout_failed`
trending to zero with no new dead letters.
