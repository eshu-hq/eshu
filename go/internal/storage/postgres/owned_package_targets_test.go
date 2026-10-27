// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"strings"
	"testing"
)

func TestListOwnedPackageDependencyTargetsQueryIsActiveAndBounded(t *testing.T) {
	t.Parallel()

	query := listOwnedPackageDependencyTargetsQuery(true, false)
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'content_entity'",
		"fact.source_system = 'git'",
		"fact.payload->>'entity_type' = 'Variable'",
		"fact.payload->'entity_metadata'->>'config_kind' = 'dependency'",
		"fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])",
		"COALESCE((fact.payload->'entity_metadata'->>'lockfile') = 'true', FALSE) AS lockfile",
		"COALESCE(fact.payload->'entity_metadata'->>'source_location', '') AS source_location",
		"SELECT DISTINCT ON (ecosystem, package_name, version)",
		"CASE WHEN source_location <> '' THEN 0 ELSE 1 END ASC",
		"ROW_NUMBER() OVER",
		"ORDER BY rotated_rank ASC, target_rank ASC",
		"LIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("listOwnedPackageDependencyTargetsQuery missing %q:\n%s", want, query)
		}
	}
}

func TestListOwnedPackageDependencyTargetsQueryCanUsePackageLevelIdentity(t *testing.T) {
	t.Parallel()

	query := listOwnedPackageDependencyTargetsQuery(false, false)
	if !strings.Contains(query, "SELECT DISTINCT ON (ecosystem, package_name)") {
		t.Fatalf("package-level target query should distinct by package identity:\n%s", query)
	}
	if strings.Contains(query, "SELECT DISTINCT ON (ecosystem, package_name, version)") {
		t.Fatalf("package-level target query should not spend package-registry budget on per-version rows:\n%s", query)
	}
}

func TestOwnedPackageDependencyTargetLimitSupportsFullCorpusBudgets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  int
		want int
	}{
		{name: "default", raw: 0, want: 100},
		{name: "historical cap no longer downshifts", raw: 1000, want: 1000},
		{name: "max", raw: 5000, want: 5000},
		{name: "over max clamps", raw: 5001, want: 5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ownedPackageDependencyTargetLimit(tc.raw); got != tc.want {
				t.Fatalf("ownedPackageDependencyTargetLimit(%d) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestOwnedPackageEcosystemAliasExpansionSharesOneTable(t *testing.T) {
	t.Parallel()

	raw := listOwnedPackageDependencyTargetsQuery(true, false)
	if strings.Contains(raw, "CASE LOWER(") {
		t.Fatalf("raw package_manager query must not fold aliases:\n%s", raw)
	}
	expanded := listOwnedPackageDependencyTargetsQuery(true, true)
	for _, want := range []string{
		"CASE LOWER(fact.payload->'entity_metadata'->>'package_manager')",
		"WHEN 'gradle' THEN 'maven'",
		"WHEN 'gomod' THEN 'go'",
		"END AS ecosystem",
		"SELECT DISTINCT ON (ecosystem, package_name, version)",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("alias-expanded query missing %q:\n%s", want, expanded)
		}
	}
	if got, want := ownedPackageManagerFilterValues([]string{"go", "maven"}, true),
		[]string{"go", "go-module", "go_module", "gomod", "gradle", "maven"}; !slices.Equal(got, want) {
		t.Fatalf("expanded filter values = %#v, want %#v", got, want)
	}
	if got, want := ownedPackageManagerFilterValues([]string{"maven"}, false), []string{"maven"}; !slices.Equal(got, want) {
		t.Fatalf("raw filter values = %#v, want %#v", got, want)
	}
}
