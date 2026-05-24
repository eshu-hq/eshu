package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const listOwnedPackageDependencyTargetsQuery = `
WITH active_dependencies AS (
  SELECT
    LOWER(fact.payload->'entity_metadata'->>'package_manager') AS ecosystem,
    fact.payload->>'entity_name' AS package_name,
    fact.payload->'entity_metadata'->>'value' AS version,
    COALESCE((fact.payload->'entity_metadata'->>'lockfile') = 'true', FALSE) AS lockfile,
    COALESCE(fact.payload->>'repo_id', '') AS repository_id,
    fact.fact_id
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
)
SELECT DISTINCT ON (ecosystem, package_name, version)
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id
FROM active_dependencies
ORDER BY ecosystem ASC, package_name ASC, version ASC, lockfile DESC, fact_id ASC
LIMIT $2
`

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
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, listOwnedPackageDependencyTargetsQuery, ecosystems, limit)
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
		); err != nil {
			return nil, fmt.Errorf("list owned package dependency targets: %w", err)
		}
		target.Ecosystem = strings.ToLower(strings.TrimSpace(target.Ecosystem))
		target.PackageName = strings.TrimSpace(target.PackageName)
		target.Version = strings.TrimSpace(target.Version)
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
