// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	defaultOwnedPackageDependencyTargetLimit = 100
	maxOwnedPackageDependencyTargetLimit     = 5000
)

func listOwnedPackageDependencyTargetsQuery(versionSpecific bool) string {
	distinctColumns := "ecosystem, package_name"
	distinctOrder := "ecosystem ASC, package_name ASC, CASE WHEN source_location <> '' THEN 0 ELSE 1 END ASC, lockfile DESC, version ASC, fact_id ASC"
	if versionSpecific {
		distinctColumns = "ecosystem, package_name, version"
		distinctOrder = "ecosystem ASC, package_name ASC, version ASC, CASE WHEN source_location <> '' THEN 0 ELSE 1 END ASC, lockfile DESC, fact_id ASC"
	}
	return fmt.Sprintf(`
WITH active_dependencies AS (
  SELECT
    LOWER(fact.payload->'entity_metadata'->>'package_manager') AS ecosystem,
    fact.payload->>'entity_name' AS package_name,
    fact.payload->'entity_metadata'->>'value' AS version,
    COALESCE((fact.payload->'entity_metadata'->>'lockfile') = 'true', FALSE) AS lockfile,
    COALESCE(fact.payload->>'repo_id', '') AS repository_id,
    fact.fact_id,
    COALESCE(fact.payload->'entity_metadata'->>'source_location', '') AS source_location
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'entity_type' = 'Variable'
  AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
  AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
  AND generation.status = 'active'
),
distinct_targets AS (
  SELECT DISTINCT ON (%s)
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id,
    source_location
  FROM active_dependencies
  ORDER BY %s
),
numbered_targets AS (
  SELECT
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id,
    source_location,
    ROW_NUMBER() OVER (
      ORDER BY ecosystem ASC, package_name ASC, version ASC, lockfile DESC, fact_id ASC
    ) - 1 AS target_rank,
    COUNT(*) OVER () AS total_targets
  FROM distinct_targets
),
rotated_targets AS (
  SELECT
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id,
    source_location,
    target_rank,
    MOD(target_rank - MOD($3::bigint, total_targets) + total_targets, total_targets) AS rotated_rank
  FROM numbered_targets
)
SELECT
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id,
    source_location
FROM rotated_targets
ORDER BY rotated_rank ASC, target_rank ASC
LIMIT $2
`, distinctColumns, distinctOrder)
}

// ListOwnedPackageDependencyTargets loads active Git dependency declarations
// that can bound package-registry and vulnerability-intelligence target
// planning.
func (s FactStore) ListOwnedPackageDependencyTargets(
	ctx context.Context,
	filter workflow.OwnedPackageDependencyTargetFilter,
) ([]workflow.OwnedPackageDependencyTarget, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	ecosystems := cleanStringFilterValues(filter.Ecosystems)
	if len(ecosystems) == 0 {
		return nil, nil
	}
	limit := ownedPackageDependencyTargetLimit(filter.Limit)

	rows, err := s.db.QueryContext(
		ctx,
		listOwnedPackageDependencyTargetsQuery(filter.VersionSpecific),
		ecosystems,
		limit,
		filter.RotationOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("list owned package dependency targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	targets := make([]workflow.OwnedPackageDependencyTarget, 0, limit)
	for rows.Next() {
		var target workflow.OwnedPackageDependencyTarget
		if err := rows.Scan(
			&target.Ecosystem,
			&target.PackageName,
			&target.Version,
			&target.Lockfile,
			&target.RepositoryID,
			&target.FactID,
			&target.SourceLocation,
		); err != nil {
			return nil, fmt.Errorf("list owned package dependency targets: %w", err)
		}
		target.Ecosystem = strings.ToLower(strings.TrimSpace(target.Ecosystem))
		target.PackageName = strings.TrimSpace(target.PackageName)
		target.Version = strings.TrimSpace(target.Version)
		target.SourceLocation = strings.TrimSpace(target.SourceLocation)
		if target.Ecosystem == "" || target.PackageName == "" {
			continue
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list owned package dependency targets: %w", err)
	}
	return targets, nil
}

func ownedPackageDependencyTargetLimit(raw int) int {
	if raw <= 0 {
		return defaultOwnedPackageDependencyTargetLimit
	}
	if raw > maxOwnedPackageDependencyTargetLimit {
		return maxOwnedPackageDependencyTargetLimit
	}
	return raw
}
