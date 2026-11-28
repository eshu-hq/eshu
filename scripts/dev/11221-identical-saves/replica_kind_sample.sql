-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11221: reducer fact kinds in a 1% system sample of fact_records (about 954,000 rows), with
-- their average payload size and how many were written more than an hour after their
-- generation was created. Read-only, aggregate output only; run on the READ REPLICA in a
-- session with default_transaction_read_only=on. Summary recorded on issue #11221.
SELECT f.fact_kind,
       count(*) AS sample_rows,
       round(avg(pg_column_size(f.payload))) AS avg_payload_b,
       round(100.0 * count(*) FILTER (WHERE f.ingested_at > g.ingested_at + interval '1 hour') / count(*), 1) AS pct_written_gt1h_after_generation,
       round(100.0 * count(*) FILTER (WHERE f.fencing_token > 1) / count(*), 1) AS pct_token_gt1
FROM fact_records AS f TABLESAMPLE SYSTEM (1)
JOIN scope_generations g ON g.generation_id = f.generation_id
WHERE f.fact_kind LIKE 'reducer\_%'
GROUP BY 1
ORDER BY 2 DESC;
