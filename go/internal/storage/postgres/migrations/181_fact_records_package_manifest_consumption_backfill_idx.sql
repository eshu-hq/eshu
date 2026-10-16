-- 181_fact_records_package_manifest_consumption_backfill_idx.sql
--
-- Page index for the package-consumption sidecar backfill (#7663).
--
-- listPackageManifestConsumptionKeyScopeFactsSQL
-- (package_manifest_consumption_backfill.go) reads one scope's manifest and
-- registry facts 500 at a time, ordered by fact_id after a cursor. No index
-- served that shape: fact_id is the fourth column of
-- fact_records_scope_generation_keyset_idx. Each page walked every fact of the
-- scope above the cursor, across all generations and kinds, then sorted. On a
-- 398k-fact scope with 31,836 matching facts, a generic plan cost 403k buffers
-- on page 1 and a full 64-page pass took 10.9 s. A custom plan
-- misestimated rows=9 and seq-scanned the table on early pages (24.1 s).
--
-- This partial index holds only the matching facts, keyed (scope_id, fact_id),
-- so every page is one ordered range scan with no sort and no residual filter:
-- 507 buffers per page from the first page to the last, 12 ms for all 64
-- pages, and 14 MB on the 2.6M-fact fixture. The initial pass's DISTINCT scope
-- walk (listPackageManifestConsumptionKeyScopesSQL) shares the predicate and
-- becomes an index-only scan.
--
-- The predicate is the page query's own text, byte for byte.
-- TestPackageManifestConsumptionBackfillIndexPredicateMatchesPageSQL derives it
-- from the Go constant and fails on drift. Keep this file one CREATE INDEX
-- CONCURRENTLY statement, so the migrator runs it outside a transaction and
-- fact_records writes are never blocked (#7004).

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_package_manifest_consumption_backfill_idx
    ON fact_records (scope_id, fact_id)
    WHERE (
      (fact_kind = 'content_entity'
          AND source_system = 'git'
          AND payload->>'entity_type' = 'Variable'
          AND COALESCE(NULLIF(payload->>'config_kind', ''), payload->'entity_metadata'->>'config_kind') = 'dependency')
      OR fact_kind = 'package_registry.package'
  );
