-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8835. Only noncanonical scalar values enter this partial index; exact values keep their existing raw index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_service_catalog_workload_trim_idx
    ON fact_records (BTRIM(payload->>'workload_id'))
    WHERE fact_kind = 'reducer_service_catalog_correlation' AND is_tombstone = FALSE AND jsonb_typeof(payload->'workload_id') = 'string' AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id');
