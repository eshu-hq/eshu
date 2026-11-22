-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 skew what-if: split the largest scope into k equal sub-keys and
-- report the largest part at modulus 32 and 64. Same scratch tables as
-- skew_census.sql (p and census). Placement of a renamed key is hash luck, so
-- k=1 (the giant under a new name) differs from the real placement; read the
-- k>=4 rows as the floor the other large scopes set.
\set ON_ERROR_STOP on
CREATE TEMP TABLE g AS SELECT scope_id, n FROM census ORDER BY n DESC LIMIT 1;
SELECT 'giant rows', n FROM g;
DO $$
DECLARE k int; m int; r record;
BEGIN
  FOREACH k IN ARRAY ARRAY[1,4,8,16,32] LOOP
    FOREACH m IN ARRAY ARRAY[32,64] LOOP
      SELECT max(rows), round(100.0*max(rows)/sum(rows),1) INTO r FROM (
        SELECT (SELECT min(rem) FROM generate_series(0,m-1) rem WHERE satisfies_hash_partition('p'::regclass, m, rem, sid)) rem, sum(n) rows
        FROM (
          SELECT scope_id sid, n FROM census WHERE scope_id <> (SELECT scope_id FROM g) AND n > 0
          UNION ALL SELECT (SELECT scope_id FROM g) || '#' || i, (SELECT n FROM g)/k FROM generate_series(1,k) i
        ) s GROUP BY 1) q;
      RAISE NOTICE 'giant split into % sub-keys, modulus %: largest part % rows = %%%', k, m, r.max, r.round;
    END LOOP;
  END LOOP;
END $$;
