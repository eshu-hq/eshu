-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 way back: reverse catch-up, rename back (migration_rehearsal.sh stage wayback).
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
LOCK TABLE public.fact_records_old IN ACCESS EXCLUSIVE MODE;
SELECT clock_timestamp() AS lock_acquired \gset
INSERT INTO public.fact_records_old SELECT n.* FROM public.fact_records n
  WHERE n.ingested_at >= (SELECT max(ingested_at) - interval '1 hour' FROM public.fact_records_old)
  ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload, ingested_at = EXCLUDED.ingested_at;
ALTER TABLE public.fact_records SET SCHEMA d;
ALTER TABLE public.fact_records_old RENAME TO fact_records;
SELECT round(extract(epoch FROM clock_timestamp() - :'lock_acquired'::timestamptz)::numeric, 3) AS exclusive_lock_held_seconds;
COMMIT;
