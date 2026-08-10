// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"testing"
)

// familyRegistry returns a family's registered edge types regardless of which
// registry holds it, so these tests do not have to know that repo_dependency
// lives beside its writer while workload_dependency lives in the shared table.
func familyRegistry(t *testing.T, family string) map[string]string {
	t.Helper()
	switch family {
	case "repo_dependency":
		return RepoDependencyMaterializedEdgeTypes()
	case "code_calls":
		return CodeCallMaterializedEdgeTypes()
	case "inheritance_edges":
		return InheritanceMaterializedEdgeTypes()
	default:
		reg, ok := SingleTypeMaterializedEdgeTypes(family)
		if !ok {
			t.Fatalf("family %q is not registered anywhere", family)
		}
		return reg
	}
}

// TestEndpointConstraintsCoverEveryRegisteredType is the load-bearing invariant
// for endpoint scoping (#5543).
//
// A constrained family matches an edge only when its type AND endpoints agree.
// If a registered type has no constraint entry, the natural implementation
// treats it as unmatched, so the gate silently stops asserting that type while
// the family still reports exhaustive — the same false-green shape as an
// undercounted type set, arrived at from the other direction.
//
// So constraints must be TOTAL over the family's registered types. Partial
// constraints are rejected here rather than being tolerated by a permissive
// lookup, because a permissive lookup would hide exactly the case this exists
// to catch.
func TestEndpointConstraintsCoverEveryRegisteredType(t *testing.T) {
	t.Parallel()

	for family := range materializedEdgeEndpointsByFamily {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			constraints, ok := MaterializedEdgeEndpointLabels(family)
			if !ok {
				t.Fatalf("family %q has constraints in the table but the accessor reports none", family)
			}
			registry := familyRegistry(t, family)

			var uncovered, orphaned []string
			for edgeType := range registry {
				if _, ok := constraints[edgeType]; !ok {
					uncovered = append(uncovered, edgeType)
				}
			}
			for edgeType := range constraints {
				if _, ok := registry[edgeType]; !ok {
					orphaned = append(orphaned, edgeType)
				}
			}
			sort.Strings(uncovered)
			sort.Strings(orphaned)

			if len(uncovered) > 0 {
				t.Errorf("registered types %v have no endpoint constraint; the gate would stop asserting them while still calling %s exhaustive",
					uncovered, family)
			}
			if len(orphaned) > 0 {
				t.Errorf("endpoint constraints name %v, which the family does not register; the constraint is dead and its type is asserted by nobody",
					orphaned)
			}
			for edgeType, endpoint := range constraints {
				if endpoint.FromLabel == "" || endpoint.ToLabel == "" {
					t.Errorf("edge type %q has a blank endpoint label (from=%q to=%q); a blank label would match nothing and silently drop the type",
						edgeType, endpoint.FromLabel, endpoint.ToLabel)
				}
			}
		})
	}
}

// TestSharedEdgeTypesAreDisambiguatedByEndpoints proves the collision that
// motivated endpoint scoping is actually resolved.
//
// DEPENDS_ON belongs to both repo_dependency (Repository->Repository) and
// workload_dependency (Workload->Workload). Proving both families in one batched
// live cell requires their endpoint constraints to differ; if they ever
// converged, each family's exact-set assertion would count the other's edges as
// spurious extras and both would fail with a misleading cause.
func TestSharedEdgeTypesAreDisambiguatedByEndpoints(t *testing.T) {
	t.Parallel()

	repoConstraints, ok := MaterializedEdgeEndpointLabels("repo_dependency")
	if !ok {
		t.Fatal("repo_dependency has no endpoint constraints; it shares DEPENDS_ON and needs them")
	}
	workloadConstraints, ok := MaterializedEdgeEndpointLabels("workload_dependency")
	if !ok {
		t.Fatal("workload_dependency has no endpoint constraints; it shares DEPENDS_ON and needs them")
	}

	repoDependsOn := repoConstraints["DEPENDS_ON"]
	workloadDependsOn := workloadConstraints["DEPENDS_ON"]
	if repoDependsOn == workloadDependsOn {
		t.Fatalf("both families constrain DEPENDS_ON to %+v; identical endpoints cannot partition the shared type, so each family's exact set would see the other's edges as extras",
			repoDependsOn)
	}
}

// TestUnconstrainedFamilyReportsAbsenceNotEmptiness pins the distinction the
// caller depends on.
//
// Most families own their types outright and carry no constraints. The accessor
// must say "none" so the caller matches every edge of the family's types; if it
// returned an empty map with ok=true, a caller applying constraints strictly
// would match nothing and the family's live proof would assert an empty graph.
func TestUnconstrainedFamilyReportsAbsenceNotEmptiness(t *testing.T) {
	t.Parallel()

	got, ok := MaterializedEdgeEndpointLabels("runs_in")
	if ok {
		t.Errorf("runs_in reported constraints %+v; it owns RUNS_IN outright and must report none", got)
	}
	if got != nil {
		t.Errorf("unconstrained family returned a non-nil map %+v", got)
	}
}
