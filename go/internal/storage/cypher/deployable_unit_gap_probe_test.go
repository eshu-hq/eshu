// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"strings"
	"testing"
)

// TestBuildDeployableUnitCorrelationIncidentReposProbeShape pins the #8316
// gap-reconciliation probe: one UNWIND-anchored directed outgoing match per
// admitted anchor, DISTINCT repo ids back, and none of the
// relationship-existence predicate shapes the pinned NornicDB backends
// mis-evaluate.
func TestBuildDeployableUnitCorrelationIncidentReposProbeShape(t *testing.T) {
	t.Parallel()

	stmt := BuildDeployableUnitCorrelationIncidentRepos([]string{"repo-a"})
	if stmt.Operation != OperationCanonicalProbe {
		t.Fatalf("probe Operation = %q, want %q", stmt.Operation, OperationCanonicalProbe)
	}
	for _, fragment := range []string{
		"UNWIND $repo_ids AS requested_repo_id",
		"MATCH (s:Repository {id: requested_repo_id})",
		"-[rel:CORRELATES_DEPLOYABLE_UNIT]->",
		"RETURN DISTINCT s.id AS repo_id",
	} {
		if !strings.Contains(stmt.Cypher, fragment) {
			t.Fatalf("probe Cypher missing %q:\n%s", fragment, stmt.Cypher)
		}
	}
	assertNoForbiddenRelationshipPatterns(t, "gap probe", stmt.Cypher)
	if got, ok := stmt.Parameters["repo_ids"].([]string); !ok || !reflect.DeepEqual(got, []string{"repo-a"}) {
		t.Fatalf("probe repo_ids = %#v, want [repo-a]", stmt.Parameters["repo_ids"])
	}
}

// forbiddenRelationshipPatterns are the relationship-existence predicate
// shapes proven mis-evaluated on both pinned NornicDB backends (#5147). It
// duplicates forbiddenOrphanSweepPatterns in the sweep/orphan leaf
// (sweep/orphan/sweep_test.go): test helpers cannot be imported
// across the package split, so each side carries its own copy. Keep the
// copies behavior-identical.
var forbiddenRelationshipPatterns = []string{
	")--(",
	"NOT (",
	"COUNT {",
	"EXISTS {",
}

func assertNoForbiddenRelationshipPatterns(t *testing.T, name, cypher string) {
	t.Helper()
	for _, forbidden := range forbiddenRelationshipPatterns {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("%s Cypher must not contain forbidden relationship-existence pattern %q:\n%s", name, forbidden, cypher)
		}
	}
}
