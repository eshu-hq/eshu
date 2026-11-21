-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11129: exact minimum capture time of the winners staging queue from an
-- index endpoint. Every keyed tick, the pre-lease gauge sample, and the full
-- rebuild stamp read min(captured_at) over the staging queue; with only the
-- seq index (migration 309) that read was a Parallel Seq Scan of the whole
-- queue, so a staged burst of N marks cost about N/50,000 ticks times a scan
-- of N rows (measured on a 12.16M-row burst, Postgres 18.6: 25.6 s cold).
-- With this btree the read is a one-entry Limit over the index (0.03 ms), and
-- it stays exact, which the watermark stamp needs: captured_at is
-- clock_timestamp() per row and is not monotone in seq, so no seq-ordered
-- read can supply it.
--
-- Non-unique on purpose: a unique index would put an arbiter back on the
-- plain-INSERT writer path the staging table exists to free. Writer cost
-- (12.16M marks): pure staging inserts at 8 writers run 22 to 25 percent
-- slower with this index; the real writer transaction (finding upsert plus
-- staging capture) is the measured bar, recorded in the #11129 evidence note.
--
-- CONCURRENTLY so bootstrap never blocks writers on this hot table; IF NOT
-- EXISTS for rerun safety. One statement per file so the migration
-- coordinator runs it outside a transaction on a dedicated bootstrap
-- connection, and an INVALID index left by a failed build is dropped by name
-- before the definition reruns (SQLDB.dropInvalidConcurrentIndexes). The
-- build took 3.9 s on a 12.16M-row queue. A NEW migration file per issue
-- #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_winners_dirty_staging_captured_at_idx
    ON supply_chain_impact_winners_dirty_staging (captured_at);
