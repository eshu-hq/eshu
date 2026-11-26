BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL plan_cache_mode = 'force_custom_plan';
PREPARE h2r1 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r1(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', '', 'reducer_supply_chain_impact_finding:1', ARRAY['0:CVE-2026-112943','1:CVE-2026-112943']);
DEALLOCATE h2r1;
PREPARE h2r2 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r2(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:1', 'reducer_supply_chain_impact_finding:2', ARRAY['2:CVE-2026-112943']);
DEALLOCATE h2r2;
PREPARE h2r3 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r3(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:2', 'reducer_supply_chain_impact_finding:3', ARRAY['3:CVE-2026-112943']);
DEALLOCATE h2r3;
PREPARE h2r4 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r4(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:3', 'reducer_supply_chain_impact_finding:4', ARRAY['4:CVE-2026-112943']);
DEALLOCATE h2r4;
PREPARE h2r5 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r5(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:4', 'reducer_supply_chain_impact_finding:5', ARRAY['5:CVE-2026-112943']);
DEALLOCATE h2r5;
PREPARE h2r6 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r6(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:5', 'reducer_supply_chain_impact_finding:6', ARRAY['6:CVE-2026-112943']);
DEALLOCATE h2r6;
PREPARE h2r7 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r7(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:6', 'reducer_supply_chain_impact_finding:7', ARRAY['7:CVE-2026-112943']);
DEALLOCATE h2r7;
PREPARE h2r8 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r8(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:7', 'reducer_supply_chain_impact_finding:8', ARRAY['8:CVE-2026-112943']);
DEALLOCATE h2r8;
PREPARE h2r9 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r9(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:8', 'reducer_supply_chain_impact_finding:9', ARRAY['9:CVE-2026-112943']);
DEALLOCATE h2r9;
PREPARE h2r10 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r10(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:9', 'reducer_supply_chain_impact_finding:a', ARRAY['10:CVE-2026-112943']);
DEALLOCATE h2r10;
PREPARE h2r11 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r11(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:a', 'reducer_supply_chain_impact_finding:b', ARRAY['11:CVE-2026-112943']);
DEALLOCATE h2r11;
PREPARE h2r12 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r12(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:b', 'reducer_supply_chain_impact_finding:c', ARRAY['12:CVE-2026-112943']);
DEALLOCATE h2r12;
PREPARE h2r13 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r13(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:c', 'reducer_supply_chain_impact_finding:d', ARRAY['13:CVE-2026-112943']);
DEALLOCATE h2r13;
PREPARE h2r14 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r14(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:d', 'reducer_supply_chain_impact_finding:e', ARRAY['14:CVE-2026-112943']);
DEALLOCATE h2r14;
PREPARE h2r15 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r15(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:e', 'reducer_supply_chain_impact_finding:f', ARRAY['15:CVE-2026-112943']);
DEALLOCATE h2r15;
PREPARE h2r16 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $4::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2r16(ARRAY['CVE-2026-112943'], 'h2-shim-own-scope', 'reducer_supply_chain_impact_finding:f', ARRAY['16:CVE-2026-112943']);
DEALLOCATE h2r16;
COMMIT;
