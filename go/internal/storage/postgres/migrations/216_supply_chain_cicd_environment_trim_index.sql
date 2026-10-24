-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8835. Only noncanonical scalar values enter this partial index; exact values keep their existing raw index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_ci_cd_environment_trim_idx
    ON fact_records (BTRIM(payload->>'environment'))
    WHERE fact_kind = 'reducer_ci_cd_run_correlation' AND is_tombstone = FALSE AND jsonb_typeof(payload->'environment') = 'string' AND payload->>'environment' IS DISTINCT FROM BTRIM(payload->>'environment');
