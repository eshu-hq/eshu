//go:build ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"
)

// TestCanonicalCloudResourceUpsertCypherIncludesTeethClauseUnderBuildTag
// proves the ifadeterminismteeth build tag actually wires
// teethCloudResourceUpsertExtraSet into canonicalCloudResourceUpsertCypher.
// Only `go test -tags ifadeterminismteeth ./...` compiles this file; see
// cloud_resource_node_writer_teeth.go's doc for why this SET clause exists
// and cloud_resource_node_writer_test.go's
// TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault for the
// counterpart regression guard that runs in every normal build.
func TestCanonicalCloudResourceUpsertCypherIncludesTeethClauseUnderBuildTag(t *testing.T) {
	t.Parallel()

	want := "r.ifa_teeth_write_order = row.ifa_teeth_write_order"
	if !strings.Contains(canonicalCloudResourceUpsertCypher, want) {
		t.Fatalf("canonicalCloudResourceUpsertCypher missing %q under ifadeterminismteeth:\n%s", want, canonicalCloudResourceUpsertCypher)
	}
}
