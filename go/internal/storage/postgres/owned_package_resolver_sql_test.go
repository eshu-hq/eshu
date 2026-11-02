// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestOwnedResolverPartsAreSlicesOfProduction pins that every resolver SQL
// piece is a byte slice of the shipped reader statements, so the resolver can
// never keep a stale copy of the reader's row selection, identity, slot key or
// folded-name predicate.
func TestOwnedResolverPartsAreSlicesOfProduction(t *testing.T) {
	t.Parallel()
	parts, err := ownedResolverPartsOnce()
	if err != nil {
		t.Fatalf("ownedResolverPartsOnce: %v", err)
	}
	reader := listOwnedPackageDependencyTargetsQuery(true, true)
	for name, piece := range map[string]string{
		"activeDependencies": parts.activeDependencies,
		"distinctColumns":    parts.distinctColumns,
		"distinctOrder":      parts.distinctOrder,
		"slotKey":            parts.slotKey,
		"low24":              parts.low24,
	} {
		if strings.TrimSpace(piece) == "" || !strings.Contains(reader, piece) {
			t.Errorf("%s = %q is empty or not a slice of the owned-package reader statement", name, piece)
		}
	}
	if !strings.Contains(listActivePackageManifestDependencyFactsQuery, parts.foldedName+" = ANY($2::text[])") {
		t.Errorf("foldedName = %q is not the manifest reader's folded predicate", parts.foldedName)
	}
	if !strings.Contains(listActivePackageManifestDependencyFactsWithSBTQuery, parts.foldedName+" = ANY($2::text[])") {
		t.Errorf("foldedName = %q is not the Maven-family reader's folded predicate", parts.foldedName)
	}
	// The reader hashes the raw stored name; a lower-cased or folded key would
	// be a silent slot miss.
	if strings.Contains(strings.ToLower(parts.slotKey), "lower(") || !strings.Contains(parts.slotKey, "package_name") {
		t.Errorf("slotKey = %q must hash the raw package_name", parts.slotKey)
	}
	if !strings.Contains(parts.activeDependencies, "ingestion_scopes") || !strings.Contains(parts.activeDependencies, "is_tombstone = FALSE") {
		t.Errorf("activeDependencies must keep the active-generation and tombstone selection")
	}
}

func TestOwnedResolverStatementsKeepTheFoldedSeekShape(t *testing.T) {
	t.Parallel()
	parts, err := ownedResolverPartsOnce()
	if err != nil {
		t.Fatalf("ownedResolverPartsOnce: %v", err)
	}
	byName := ownedResolverByNameQuery(parts)
	for _, want := range []string{"CROSS JOIN LATERAL", "OFFSET 0", parts.foldedName + " = names.name", "package_manager' = ANY($1::text[])"} {
		if !strings.Contains(byName, want) {
			t.Errorf("by-name statement lacks %q", want)
		}
	}
	if strings.Contains(byName, "= ANY(SELECT") || strings.Contains(byName, "= ANY (SELECT") {
		t.Errorf("by-name statement must not read names as ANY(subquery)")
	}
	swift := ownedResolverSwiftQuery(parts)
	if !strings.Contains(swift, "package_manager' = ANY($1::text[])") || strings.Contains(swift, "$2") || strings.Contains(swift, "'swift'") {
		t.Errorf("swift statement must select its prefix from the $1 alias set and bind nothing else")
	}
	pypi := ownedResolverPyPIQuery(parts)
	for _, want := range []string{"regexp_replace(" + parts.foldedName + ", '[-_.]+', '-', 'g') = ANY($2::text[])", "package_manager' = ANY($1::text[])"} {
		if !strings.Contains(pypi, want) {
			t.Errorf("pypi statement lacks %q", want)
		}
	}
	if strings.Contains(pypi, "LATERAL") {
		t.Errorf("pypi statement is a prefix read, not a per-name seek")
	}
	if !strings.Contains(ownedResolverBinaryArtifactQuery(parts), "sbt_binary_artifact") {
		t.Errorf("binary-artifact statement must filter on sbt_binary_artifact")
	}
}

func TestBetweenReportsAMissingMarker(t *testing.T) {
	t.Parallel()
	if _, err := between("abc", "x", "c"); err == nil {
		t.Error("missing start marker must be an error")
	}
	if _, err := between("abc", "a", "x"); err == nil {
		t.Error("missing end marker must be an error")
	}
	if got, err := between("a[b]c", "[", "]"); err != nil || got != "b" {
		t.Errorf("between = %q, %v", got, err)
	}
}
