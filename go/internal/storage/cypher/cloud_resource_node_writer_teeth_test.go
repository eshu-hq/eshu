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
// teethCloudResourceUpsertExtraSet into canonicalCloudResourceUpsertCypher —
// both the reintroduced ifa_teeth_seq counter clause (issue #4396 slice 6b)
// and the ifa_teeth_write_order wall-clock floor clause. Only
// `go test -tags ifadeterminismteeth ./...` compiles this file; see
// cloud_resource_node_writer_teeth.go's doc for why these SET clauses exist
// and cloud_resource_node_writer_teeth_off_test.go's
// TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault for the
// counterpart regression guard that runs in every normal (!ifadeterminismteeth)
// build.
func TestCanonicalCloudResourceUpsertCypherIncludesTeethClauseUnderBuildTag(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"r.ifa_teeth_seq = row.ifa_teeth_seq",
		"r.ifa_teeth_write_order = row.ifa_teeth_write_order",
	} {
		if !strings.Contains(canonicalCloudResourceUpsertCypher, want) {
			t.Fatalf("canonicalCloudResourceUpsertCypher missing %q under ifadeterminismteeth:\n%s", want, canonicalCloudResourceUpsertCypher)
		}
	}
}
