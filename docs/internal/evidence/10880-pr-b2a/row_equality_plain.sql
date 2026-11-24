-- row_equality_plain.sql: with the PLAIN (canonical_key) partial index present, repeat the
-- ordered-probe statement and compare it with the result saved by row_equality.sql
-- (EXCEPT both ways, all must be 0 rows). Needs the temp table b2a_cand and probe_ordered from
-- row_equality.sql's session, so it is appended to that run by row_equality.sh plain.
SELECT 'plain EXCEPT saved ordered' AS what, count(*) FROM (SELECT * FROM probe_ordered EXCEPT SELECT * FROM b2a_probe_saved) x
UNION ALL SELECT 'saved ordered EXCEPT plain', count(*) FROM (SELECT * FROM b2a_probe_saved EXCEPT SELECT * FROM probe_ordered) x;
