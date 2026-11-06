// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadinessFleetReleaseLineExpressionMatchesMigration pins the fleet
// probe's release-line expression to the expression indexes of migrations 281
// and 282: an index on any other spelling is not used, and the probe would
// silently fall back to scanning every installed package. It also pins the
// probe CTE to the same builder, so the query and the indexes cannot drift
// apart while each still matches its own copy.
func TestReadinessFleetReleaseLineExpressionMatchesMigration(t *testing.T) {
	t.Parallel()

	payloadExpr := osPackageFleetReleaseLineExpression("payload")
	factExpr := osPackageFleetReleaseLineExpression("fact.payload")
	if !strings.Contains(payloadExpr, "distro_version") {
		t.Fatalf("release-line expression does not read distro_version: %q", payloadExpr)
	}
	if strings.Contains(payloadExpr, "purl") {
		t.Fatalf("release-line expression must not read the purl: %q", payloadExpr)
	}
	for _, file := range []string{
		"281_fact_records_os_package_fleet_release_idx.sql",
		"282_fact_records_os_package_fleet_release_source_idx.sql",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "storage", "postgres", "migrations", file))
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if !strings.Contains(string(raw), payloadExpr) {
			t.Fatalf("migration %s does not contain the query's release-line expression:\n%s\nmigration:\n%s",
				file, payloadExpr, raw)
		}
		if !strings.Contains(string(raw), "CREATE INDEX CONCURRENTLY IF NOT EXISTS") {
			t.Fatalf("migration %s must build concurrently: %s", file, raw)
		}
		if !strings.Contains(string(raw), "WHERE fact_kind = 'vulnerability.os_package'") ||
			!strings.Contains(string(raw), "AND is_tombstone = FALSE") {
			t.Fatalf("migration %s must keep the active os_package partial predicate", file)
		}
	}
	if !strings.Contains(readinessDerivedReleaseFleetProbeCTE(), factExpr) {
		t.Fatal("the fleet probe does not use the release-line expression")
	}
	// The probe binds the expression against fact.payload while the indexes
	// carry it against payload: the spellings must differ only in the table
	// qualifier, or the index cannot serve the probe.
	if strings.ReplaceAll(factExpr, "fact.payload", "payload") != payloadExpr {
		t.Fatalf("fact-qualified expression is not the payload-qualified one with a qualifier:\n%s\n%s",
			factExpr, payloadExpr)
	}
}
