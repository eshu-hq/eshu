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
		"COUNT(*) OVER () AS total_targets",
		"sha256(convert_to(COALESCE(ecosystem, '') || '|' || COALESCE(package_name, '') || '|' || COALESCE(version, ''), 'UTF8')) AS key_hash",
		"MOD(key_hash_low24::bigint, slot_count) AS slot_of_key",
		"MOD($3::bigint, slot_count) AS window_slot",
		"COUNT(*) OVER (PARTITION BY slot_of_key) AS slot_size",
		"WHERE slot_of_key = window_slot",
		"ORDER BY target_rank ASC\nLIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("listOwnedPackageDependencyTargetsQuery missing %q:\n%s", want, query)
		}
	}
	// The rotating offset window is gone (#9422), and md5 is unavailable on a
	// FIPS server, so neither may come back.
	for _, banned := range []string{"rotated_rank", "md5(", "$3::bigint, total_targets"} {
		if strings.Contains(query, banned) {
			t.Fatalf("listOwnedPackageDependencyTargetsQuery contains %q:\n%s", banned, query)
		}
	}
}

// TestOwnedPackageSlotDifferentialDerivesFromTheShippedQuery is the prefix
// guard for the live differential: the all-slots query must be the shipped
// statement up to the final SELECT, byte for byte, for both identity modes.
func TestOwnedPackageSlotDifferentialDerivesFromTheShippedQuery(t *testing.T) {
	t.Parallel()

	for _, versionSpecific := range []bool{true, false} {
		query := listOwnedPackageDependencyTargetsQuery(versionSpecific, true)
		if got := strings.Count(query, ownedPackageSlotSelectMarker); got != 1 {
			t.Fatalf("final SELECT marker appears %d times, want exactly 1:\n%s", got, query)
		}
		derived := ownedPackageAllSlotsQuery(versionSpecific, true)
		prefix, ok := strings.CutSuffix(derived, ownedPackageAllSlotsTail)
		if !ok {
			t.Fatalf("differential query does not end with the all-slots tail")
		}
		if !strings.HasPrefix(query, prefix) {
			t.Fatalf("differential prefix is not a byte-prefix of the shipped query")
		}
		if !strings.HasSuffix(prefix, "\n") || !strings.Contains(prefix, "window_targets AS (") {
			t.Fatalf("differential prefix does not end at the CTE chain:\n%s", prefix)
		}
	}
	if !strings.Contains(listOwnedPackageDependencyTargetsQuery(false, false),
		"sha256(convert_to(COALESCE(ecosystem, '') || '|' || COALESCE(package_name, ''), 'UTF8'))") {
		t.Fatalf("package-level query must hash the package identity without the version")
	}
}

// ownedPackageAllSlotsTail replaces the shipped final SELECT with a read-only
// tail that returns every target with its slot, so a live test can compare the
// reader's per-window rows with the whole partition.
const ownedPackageAllSlotsTail = `SELECT ecosystem, package_name, version, slot_of_key, slot_count, total_targets, slot_size
FROM window_targets
ORDER BY target_rank ASC
LIMIT $2
`

// ownedPackageAllSlotsQuery derives the differential query from the shipped
// statement by truncating it at the final SELECT marker.
func ownedPackageAllSlotsQuery(versionSpecific bool, expandAliases bool) string {
	query := listOwnedPackageDependencyTargetsQuery(versionSpecific, expandAliases)
	chain, _, found := strings.Cut(query, ownedPackageSlotSelectMarker)
	if !found {
		// The hermetic prefix guard fails first with the reason; return a
		// query that cannot run so a live caller fails too.
		return "SELECT missing_final_select_marker"
	}
	return chain + ownedPackageAllSlotsTail
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
