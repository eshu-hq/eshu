BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL plan_cache_mode = 'force_custom_plan';
\timing on
PREPARE lane2_kr0(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr0(ARRAY['CVE-2026-42043']::text[], 'scope:none', '', 'reducer_supply_chain_impact_finding:1', '{}'::text[]);
DEALLOCATE lane2_kr0;
PREPARE lane2_kr1(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr1(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:1', 'reducer_supply_chain_impact_finding:2', '{}'::text[]);
DEALLOCATE lane2_kr1;
PREPARE lane2_kr2(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr2(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:2', 'reducer_supply_chain_impact_finding:3', '{}'::text[]);
DEALLOCATE lane2_kr2;
PREPARE lane2_kr3(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr3(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:3', 'reducer_supply_chain_impact_finding:4', '{}'::text[]);
DEALLOCATE lane2_kr3;
PREPARE lane2_kr4(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr4(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:4', 'reducer_supply_chain_impact_finding:5', '{}'::text[]);
DEALLOCATE lane2_kr4;
PREPARE lane2_kr5(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr5(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:5', 'reducer_supply_chain_impact_finding:6', '{}'::text[]);
DEALLOCATE lane2_kr5;
PREPARE lane2_kr6(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr6(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:6', 'reducer_supply_chain_impact_finding:7', '{}'::text[]);
DEALLOCATE lane2_kr6;
PREPARE lane2_kr7(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr7(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:7', 'reducer_supply_chain_impact_finding:8', '{}'::text[]);
DEALLOCATE lane2_kr7;
PREPARE lane2_kr8(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr8(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:8', 'reducer_supply_chain_impact_finding:9', '{}'::text[]);
DEALLOCATE lane2_kr8;
PREPARE lane2_kr9(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr9(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:9', 'reducer_supply_chain_impact_finding:a', '{}'::text[]);
DEALLOCATE lane2_kr9;
PREPARE lane2_kr10(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr10(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:a', 'reducer_supply_chain_impact_finding:b', '{}'::text[]);
DEALLOCATE lane2_kr10;
PREPARE lane2_kr11(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr11(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:b', 'reducer_supply_chain_impact_finding:c', '{}'::text[]);
DEALLOCATE lane2_kr11;
PREPARE lane2_kr12(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr12(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:c', 'reducer_supply_chain_impact_finding:d', '{}'::text[]);
DEALLOCATE lane2_kr12;
PREPARE lane2_kr13(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr13(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:d', 'reducer_supply_chain_impact_finding:e', '{}'::text[]);
DEALLOCATE lane2_kr13;
PREPARE lane2_kr14(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr14(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:e', 'reducer_supply_chain_impact_finding:f', '{}'::text[]);
DEALLOCATE lane2_kr14;
PREPARE lane2_kr15(text[], text, text, text[]) AS WITH candidates AS MATERIALIZED (
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
EXECUTE lane2_kr15(ARRAY['CVE-2026-42043']::text[], 'scope:none', 'reducer_supply_chain_impact_finding:f', '{}'::text[]);
DEALLOCATE lane2_kr15;
COMMIT;
