-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10135: the supply-chain impact readiness fleet probe retires a derived
-- release scope whose release left the fleet: a non-fresh Alpine release row
-- stops counting when no active vulnerability.os_package fact still carries
-- that release for the package. The probe is one batched EXISTS over the
-- distinct (package, release) triples a request resolved, so it needs a point
-- lookup on exactly the two predicates it binds. This index serves the
-- installed-binary-name arm: the row's name and the release line the
-- coordinator mints from its distro_version (3.18.4 becomes v3.18).
--
-- The expression text must stay identical to
-- osPackageFleetReleaseLineExpression in
-- go/internal/query/supply/chain/impact/readiness_postgres_query_release.go
-- (TestReadinessFleetReleaseLineExpressionMatchesMigration pins the three
-- together): an index on any other spelling is not used, and the probe falls
-- back to scanning every installed package.
--
-- Build concurrently to keep fact writes available. Keep this the only
-- statement in the file: a concurrent index build cannot run inside a
-- transaction block, and the bootstrap sends each file as one string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_os_package_fleet_release_idx
    ON fact_records ((payload->>'name'), ((CASE WHEN SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1) LIKE '%.%' AND SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 1) ~ '^[0-9]+$' AND SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 2) ~ '^[0-9]+$' THEN 'v' || SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 1) || '.' || SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 2) ELSE NULL END)))
    WHERE fact_kind = 'vulnerability.os_package'
      AND is_tombstone = FALSE;
