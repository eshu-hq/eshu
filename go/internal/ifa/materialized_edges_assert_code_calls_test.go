// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestCodeCallsDomainResolvesItsMaterializedEdgeTypes pins the registry entry
// the live baseline gate needs for the code_calls family (#5991).
//
// MaterializedEdgeDomainEdgeTypes is what `eshu-ifa assert-edges -domain` reads
// to learn which edge types a family may have materialized. Without an entry the
// command errors, so the family can never be proven on ifa-determinism no matter
// how complete its Odù, guard, or expected-edge set is — which is why every one
// of the twelve remaining #5543 families is blocked here rather than on fixtures.
//
// The set is FOUR types, not one. The domain's write path
// (edge_writer_code_call_labels.go) reaches CALLS, REFERENCES, USES_METACLASS,
// and INSTANTIATES, and the default retract disjunction in canonical_retract.go
// names the same four. An earlier draft of this test asserted CALLS alone,
// reading only the first MERGE template in canonical_code_call_edges.go; that is
// the precise failure #5991 warns about, because a too-small type set makes the
// live gate ignore three quarters of the family's real edges and call the
// remainder exhaustive.
func TestCodeCallsDomainResolvesItsMaterializedEdgeTypes(t *testing.T) {
	t.Parallel()

	got, err := MaterializedEdgeDomainEdgeTypes("code_calls")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(%q) errored: %v; the live baseline gate cannot assert a family it cannot resolve", "code_calls", err)
	}
	want := []string{"CALLS", "INSTANTIATES", "REFERENCES", "USES_METACLASS"}
	if diff := compareEdgeTypeSet(want, got); diff != "" {
		t.Errorf("code_calls edge types: %s", diff)
	}
}

// TestCodeCallsDomainEdgeTypesComeFromTheWriterRegistry proves the set is
// registry-derived rather than hand-listed in the ifa package.
//
// MaterializedEdgeDomainEdgeTypes documents itself as registry-derived (#5330):
// the sql_relationships set reads straight from the writer registry so a new
// edge type is asserted without a second edit. code_calls must hold the same
// property, or the day someone adds a fifth code-call edge type the live gate
// keeps certifying a four-type graph as complete.
func TestCodeCallsDomainEdgeTypesComeFromTheWriterRegistry(t *testing.T) {
	t.Parallel()

	reg := cypher.CodeCallMaterializedEdgeTypes()
	if len(reg) == 0 {
		t.Fatal("cypher.CodeCallMaterializedEdgeTypes() is empty; an empty registry makes any graph vacuously pass the live gate")
	}
	got, err := MaterializedEdgeDomainEdgeTypes("code_calls")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes: %v", err)
	}
	if len(got) != len(reg) {
		t.Fatalf("resolver returned %d edge type(s) but the writer registry has %d; the ifa set is not derived from the registry", len(got), len(reg))
	}
	for edgeType := range reg {
		if _, ok := got[edgeType]; !ok {
			t.Errorf("writer registry names %q but the resolver omits it", edgeType)
		}
	}
}

// TestInheritanceEdgesDomainResolvesItsMaterializedEdgeTypes pins the second
// family unblocked for live baseline proof (#5996).
//
// Four types again, and again a naive read scores it as one: only INHERITS is
// written by the first template in canonical_inheritance_edges.go, with
// OVERRIDES and ALIASES further down the same file and IMPLEMENTS in
// canonical_implements_edges.go. Two families examined, two that a
// first-template reading undercounts fourfold — which is why every remaining
// #5543 family gets its set derived from the writer registry and pinned against
// the retract, never eyeballed.
func TestInheritanceEdgesDomainResolvesItsMaterializedEdgeTypes(t *testing.T) {
	t.Parallel()

	got, err := MaterializedEdgeDomainEdgeTypes("inheritance_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(%q) errored: %v; the live baseline gate cannot assert a family it cannot resolve", "inheritance_edges", err)
	}
	want := []string{"ALIASES", "IMPLEMENTS", "INHERITS", "OVERRIDES"}
	if diff := compareEdgeTypeSet(want, got); diff != "" {
		t.Errorf("inheritance_edges edge types: %s", diff)
	}
}

// TestEverySingleTypeFamilyResolvesThroughTheResolver proves the table is
// reachable through the function the live gate actually calls.
//
// The registry living in the cypher package proves nothing on its own: the gate
// reads MaterializedEdgeDomainEdgeTypes, so a table that exists but is not
// consulted would leave every family still erroring while the cypher-side tests
// stayed green — coverage that tests the wrong end of the seam.
func TestEverySingleTypeFamilyResolvesThroughTheResolver(t *testing.T) {
	t.Parallel()

	families := cypher.SingleTypeMaterializedEdgeFamilyNames()
	if len(families) == 0 {
		t.Fatal("no single-type families registered; this test would pass vacuously")
	}
	for _, family := range families {
		got, err := MaterializedEdgeDomainEdgeTypes(family)
		if err != nil {
			t.Errorf("family %q does not resolve through the resolver the live gate calls: %v", family, err)
			continue
		}
		if len(got) == 0 {
			t.Errorf("family %q resolved to an empty type set; the live gate would assert nothing and pass any graph", family)
		}
	}
}

// TestUnregisteredFamilyStillFailsClosed keeps the default arm honest.
//
// Registering families one at a time is only safe while an unregistered one
// still errors: if the default ever returned an empty set instead, every
// not-yet-covered family would vacuously pass the live gate with zero edges
// asserted — the exact false-green the waiver rows exist to prevent.
func TestUnregisteredFamilyStillFailsClosed(t *testing.T) {
	t.Parallel()

	// repo_dependency is deliberately the example: it is the one #5543 family
	// still unregistered, because its dispatch writes DEPENDS_ON, RUNS_ON, and a
	// generic repo-relationship fallthrough reaped by three different retracts,
	// and that ownership question is unresolved. Until it is settled the family
	// must fail closed rather than resolve to a guessed set.
	got, err := MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err == nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(%q) returned %v with no error; an unregistered family must fail closed, not assert an empty set", "repo_dependency", keysOfSet(got))
	}
	if !strings.Contains(err.Error(), "repo_dependency") {
		t.Errorf("error %q does not name the unresolved family; the gate operator cannot tell which family is unregistered", err)
	}
}

// keysOfSet renders a set as a slice so failures name the actual types.
func keysOfSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// compareEdgeTypeSet reports set differences in both directions, naming the
// actual types so a failure is actionable without a debugger.
func compareEdgeTypeSet(want []string, got map[string]struct{}) string {
	gotList := make([]string, 0, len(got))
	for k := range got {
		gotList = append(gotList, k)
	}
	sort.Strings(gotList)

	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	var missing, extra []string
	for _, w := range want {
		if _, ok := got[w]; !ok {
			missing = append(missing, w)
		}
	}
	for _, g := range gotList {
		if _, ok := wantSet[g]; !ok {
			extra = append(extra, g)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("got [" + strings.Join(gotList, " ") + "]")
	if len(missing) > 0 {
		// MISSING is the dangerous direction: the live gate stops asserting
		// these edges and a family half-materializing still reads as exhaustive.
		b.WriteString("; MISSING " + strings.Join(missing, ","))
	}
	if len(extra) > 0 {
		// EXTRA claims edges this family does not own, so another family's
		// regression would surface as a spurious failure here.
		b.WriteString("; EXTRA " + strings.Join(extra, ","))
	}
	return b.String()
}
