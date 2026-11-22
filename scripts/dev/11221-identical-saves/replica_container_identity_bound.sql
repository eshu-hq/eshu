-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11221 F1: the one bounded read of the container image identity row volume. Read-only, on the
-- READ REPLICA, in a session with default_transaction_read_only=on. The count is NOT run: the plan
-- below does not use the partial index (scope_id, generation_id, fact_id) WHERE fact_kind = the kind,
-- so it would read the whole scope_generation index. These statements read the planner and the
-- table statistics only (no table scan). Output as read: docs/internal/evidence/11221-identical-saves/
-- container-identity-volume.txt
EXPLAIN SELECT scope_id, generation_id, count(*)
FROM fact_records WHERE fact_kind = 'reducer_container_image_identity' GROUP BY 1, 2;

-- Table size, the number of most-common values kept for fact_kind, and their smallest frequency.
-- A kind that is not among the most common values holds fewer rows than smallest_frequency * reltuples.
SELECT (SELECT reltuples::bigint FROM pg_class WHERE oid = 'fact_records'::regclass) AS reltuples,
       array_length(most_common_vals::text::text[], 1) AS n_most_common_values,
       (SELECT min(f) FROM unnest(most_common_freqs) f) AS smallest_frequency,
       array_position(most_common_vals::text::text[], 'reducer_container_image_identity') AS kind_position_in_most_common
FROM pg_stats WHERE tablename = 'fact_records' AND attname = 'fact_kind';

-- Distinct (scope, generation) pairs the domain has work items for.
SELECT count(*) FILTER (WHERE status = 'succeeded') AS succeeded_items,
       count(DISTINCT (scope_id, generation_id)) AS distinct_scope_generation_pairs
FROM fact_work_items WHERE stage = 'reducer' AND domain = 'container_image_identity';
