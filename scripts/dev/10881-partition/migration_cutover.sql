-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 cutover: lock, catch-up of the delta, rename swap (migration_rehearsal.sh stage cutover).
\timing off
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
SELECT clock_timestamp() AS lock_acquired \gset
INSERT INTO d.fact_records SELECT o.* FROM public.fact_records o
  WHERE o.ingested_at >= (SELECT max(ingested_at) - interval '1 hour' FROM d.fact_records)
  ON CONFLICT (scope_id, fact_id) DO UPDATE SET payload = EXCLUDED.payload, ingested_at = EXCLUDED.ingested_at;
ALTER TABLE public.fact_records RENAME TO fact_records_old;
ALTER TABLE d.fact_records SET SCHEMA public;
SELECT round(extract(epoch FROM clock_timestamp() - :'lock_acquired'::timestamptz)::numeric, 3) AS exclusive_lock_held_seconds;
COMMIT;
