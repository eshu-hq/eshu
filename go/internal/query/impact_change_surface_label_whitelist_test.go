// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestChangeSurfaceScopedDropsLabelsTheServerFailedToFilter pins the Go-side
// enforcement of the impacted-label whitelist on the scoped traversal.
//
// The scoped Cypher expresses that whitelist as a WHERE attached to a WITH
// (impact_change_surface_traversal.go), which the pinned NornicDB build does not
// evaluate as a filter: label tests in that clause position are silently
// dropped, so the backend returns every reachable node. The rows below are what
// that backend actually hands back — File and Function are reachable one hop
// from a Repository and are not in the whitelist.
func TestChangeSurfaceScopedDropsLabelsTheServerFailedToFilter(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:owner"}, nil)
	handler := &ImpactHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		cypher string,
		_ map[string]any,
	) ([]map[string]any, error) {
		if !strings.Contains(cypher, "WITH path, impacted") {
			t.Fatalf("expected the scoped traversal, got:\n%s", cypher)
		}
		return []map[string]any{
			changeSurfaceTestRow("workload:checkout", "checkout",
				[]any{"Workload"}, "repository:owner", "DEFINES"),
			changeSurfaceTestRow("file:main.go", "main.go",
				[]any{"File"}, "repository:owner", "DEFINES"),
			changeSurfaceTestRow("function:handler", "handler",
				[]any{"Function"}, "repository:owner", "DEFINES"),
			changeSurfaceTestRow("cloudresource:bucket", "bucket",
				[]any{"CloudResource"}, "repository:owner", "DEFINES"),
		}, nil
	}}}

	rows, _, err := handler.changeSurfaceTraversalRows(
		context.Background(),
		changeSurfaceTargetCandidate{ID: "workload:changed", Labels: []string{"Workload"}},
		"",
		4,
		10,
		access,
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}

	want := []string{"cloudresource:bucket", "workload:checkout"}
	if got := changeSurfaceTestIDs(rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("row ids = %#v, want %#v -- a node outside the impacted-label "+
			"whitelist reached the caller", got, want)
	}
}

// TestChangeSurfaceKeepsEveryWhitelistedLabel guards the opposite failure: a
// filter tight enough to drop legitimate impacted kinds. Every label the legacy
// server-side whitelist admits must survive the Go filter, including Repository,
// which the scoped Cypher admits through its second CALL arm rather than through
// the WITH clause.
func TestChangeSurfaceKeepsEveryWhitelistedLabel(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:owner"}, nil)
	rows := []map[string]any{
		changeSurfaceTestRow("workload:a", "a", []any{"Workload"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("instance:b", "b", []any{"WorkloadInstance"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("cloud:c", "c", []any{"CloudResource"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("tf:d", "d", []any{"TerraformModule"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("data:e", "e", []any{"DataAsset"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("repository:owner", "f", []any{"Repository"}, "", "DEFINES"),
	}

	filtered := changeSurfaceFilterTraversalRows(rows, "", access, false)
	if got, want := len(filtered), len(rows); got != want {
		t.Fatalf("kept %d of %d whitelisted rows: %#v",
			got, want, changeSurfaceTestIDs(filtered))
	}
}

// TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher keeps the Go whitelist and
// the legacy server-side whitelist from drifting apart. The legacy query filters
// in a MATCH-attached WHERE, the clause position the pinned backend evaluates
// correctly, so its list is the contract both paths owe callers.
func TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher(t *testing.T) {
	t.Parallel()

	for label := range changeSurfaceImpactedLabels {
		if !strings.Contains(changeSurfaceLegacyCypher, "'"+label+"'") {
			t.Errorf("label %q is admitted in Go but absent from the legacy Cypher whitelist", label)
		}
	}
	clause := changeSurfaceLegacyCypher
	start := strings.Index(clause, "label IN [")
	if start < 0 {
		t.Fatal("legacy cypher no longer carries a label whitelist; update this guard")
	}
	end := strings.Index(clause[start:], "]")
	for _, quoted := range strings.Split(clause[start+len("label IN ["):start+end], ",") {
		label := strings.Trim(strings.TrimSpace(quoted), "'")
		if _, ok := changeSurfaceImpactedLabels[label]; !ok {
			t.Errorf("label %q is admitted by the legacy Cypher but dropped by the Go filter", label)
		}
	}
}
