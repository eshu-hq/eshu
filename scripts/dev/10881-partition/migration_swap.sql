-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 swap only: the catalog part of the cutover with the delta already drained, then back.
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
SELECT clock_timestamp() AS lock_acquired \gset
ALTER TABLE public.fact_records RENAME TO fact_records_old;
ALTER TABLE d.fact_records SET SCHEMA public;
SELECT round(extract(epoch FROM clock_timestamp() - :'lock_acquired'::timestamptz)::numeric, 3) AS swap_exclusive_lock_held_seconds;
COMMIT;
BEGIN;
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
ALTER TABLE public.fact_records SET SCHEMA d;
ALTER TABLE public.fact_records_old RENAME TO fact_records;
COMMIT;
