// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifadeterminismteeth

package cypher

import (
	"strings"
	"testing"
)

// TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault proves
// the ifadeterminismteeth build tag's extra SET clause
// (cloud_resource_node_writer_teeth.go) is absent from every normal build:
// this test file carries the !ifadeterminismteeth build tag, so it runs in
// the default `go test` and CI lane, where teethCloudResourceUpsertExtraSet
// must resolve to the empty string from
// cloud_resource_node_writer_teeth_off.go. It is the regression guard for
// issue #4396's determinism-matrix "--teeth" fault never leaking into a
// production build. Its counterpart,
// TestCanonicalCloudResourceUpsertCypherIncludesTeethClauseUnderBuildTag in
// cloud_resource_node_writer_teeth_test.go, asserts the opposite under the
// ifadeterminismteeth tag.
func TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{"ifa_teeth_write_order", "ifa_teeth_seq"} {
		if strings.Contains(canonicalCloudResourceUpsertCypher, forbidden) {
			t.Fatalf("canonicalCloudResourceUpsertCypher must not reference %s outside the ifadeterminismteeth build tag:\n%s", forbidden, canonicalCloudResourceUpsertCypher)
		}
	}
	if canonicalCloudResourceUpsertCypher != baseCloudResourceUpsertCypher {
		t.Fatalf("canonicalCloudResourceUpsertCypher must equal baseCloudResourceUpsertCypher outside the ifadeterminismteeth build tag")
	}
}
