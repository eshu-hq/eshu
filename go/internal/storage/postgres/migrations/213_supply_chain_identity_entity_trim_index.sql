-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8835. The padded-entity fallback still checks the original normalized value; this index bounds its work to padded identity facts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_workload_identity_entity_trim_idx
    ON fact_records (fact_id)
    WHERE fact_kind = 'reducer_workload_identity' AND is_tombstone = FALSE AND jsonb_path_exists(payload->'entity_keys', 'lax $[*] ? (@.type() == "string" && @ like_regex "(^ | $)")');
