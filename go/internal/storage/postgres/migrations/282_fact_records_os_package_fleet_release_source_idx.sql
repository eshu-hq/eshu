-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10135: the source arm of the readiness fleet probe (see migration 281).
-- An installed row built from another package's source (libssl3 from
-- openssl) carries the source name beside its binary name, and the planner
-- reads both arms when it derives targets. Absence must mean neither arm
-- still carries the release, or a source-only install would read retired
-- while the release is still in the fleet. This index serves the
-- source_package arm with the same release-line expression as 281.
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
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_os_package_fleet_release_source_idx
    ON fact_records ((payload->>'source_package'), ((CASE WHEN SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1) LIKE '%.%' AND SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 1) ~ '^[0-9]+$' AND SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 2) ~ '^[0-9]+$' THEN 'v' || SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 1) || '.' || SPLIT_PART(SPLIT_PART(TRIM(payload->>'distro_version'), ' ', 1), '.', 2) ELSE NULL END)))
    WHERE fact_kind = 'vulnerability.os_package'
      AND is_tombstone = FALSE;
