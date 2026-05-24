package postgres

import (
	"strings"
	"testing"
)

func TestListOwnedPackageDependencyTargetsQueryIsActiveAndBounded(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'content_entity'",
		"fact.source_system = 'git'",
		"fact.payload->>'entity_type' = 'Variable'",
		"fact.payload->'entity_metadata'->>'config_kind' = 'dependency'",
		"fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])",
		"COALESCE((fact.payload->'entity_metadata'->>'lockfile') = 'true', FALSE) AS lockfile",
		"SELECT DISTINCT ON (ecosystem, package_name, version)",
		"ORDER BY ecosystem ASC, package_name ASC, version ASC, lockfile DESC, fact_id ASC",
		"LIMIT $2",
	} {
		if !strings.Contains(listOwnedPackageDependencyTargetsQuery, want) {
			t.Fatalf("listOwnedPackageDependencyTargetsQuery missing %q:\n%s", want, listOwnedPackageDependencyTargetsQuery)
		}
	}
}
