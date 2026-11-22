-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 skew census at production scale. Needs a scratch database holding:
--   CREATE TABLE p(scope_id text) PARTITION BY HASH (scope_id);
--   CREATE TABLE census(scope_id text, kind text, n bigint);
--   \copy census FROM 'scope_counts.csv' CSV      (census_replica.sh output)
-- and prints, for modulus 32 and 64, how PostgreSQL's own hash function
-- (satisfies_hash_partition) spreads the real per-scope row counts over the
-- partitions. Hash partitioning divides scopes, never one scope's rows.
with m as (select unnest(array[32,64]) modulus),
r as (
  select m.modulus, c.scope_id, c.n,
         (select min(rem) from generate_series(0, m.modulus-1) rem
           where satisfies_hash_partition('p'::regclass, m.modulus, rem, c.scope_id)) rem
  from m cross join census c where c.n > 0),
agg as (select modulus, rem, sum(n) rows, count(*) scopes from r group by 1,2)
select modulus,
       count(*) parts_nonempty,
       sum(rows) total_rows,
       max(rows) largest_part_rows,
       round(100.0*max(rows)/sum(rows),1) largest_part_pct,
       round(100.0*min(rows)/sum(rows),2) smallest_part_pct,
       round(100.0/modulus,2) ideal_pct,
       max(scopes) max_scopes_in_part
from agg group by modulus order by modulus;
