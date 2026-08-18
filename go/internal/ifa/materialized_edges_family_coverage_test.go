// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// materializedEdgeFamiliesUnderUmbrella lists every family the #5543 umbrella
// tracks, so "all of them resolve" is asserted against a written-down roster
// rather than against whatever happens to be registered.
//
// Deriving this list from the registry would make the test circular: removing a
// family would shrink both sides and stay green. sql_relationships is included
// because it is the reference family the other thirteen are modelled on.
var materializedEdgeFamiliesUnderUmbrella = []string{
	"sql_relationships",
	"code_calls",
	"codeowners_ownership_edges",
	"deployable_unit_edges",
	"documentation_edges",
	"handles_route",
	"inheritance_edges",
	"invokes_cloud_action",
	"rationale_edges",
	"repo_dependency",
	"runs_in",
	"shell_exec",
	"submodule_pin_edges",
	"workload_dependency",
}

// TestEveryUmbrellaFamilyResolves is the #5543 completion criterion for the
// registry layer: the live baseline gate can address every family.
//
// Until a family resolves here, `eshu-ifa assert-edges -domain <family>` exits
// with an error and no Odù, guard, or expected-edge set can green it on
// ifa-determinism. That is what blocked all thirteen non-SQL families.
func TestEveryUmbrellaFamilyResolves(t *testing.T) {
	t.Parallel()

	for _, family := range materializedEdgeFamiliesUnderUmbrella {
		types, err := MaterializedEdgeDomainEdgeTypes(family)
		if err != nil {
			t.Errorf("family %q does not resolve: %v", family, err)
			continue
		}
		if len(types) == 0 {
			t.Errorf("family %q resolved to an empty type set; the live gate would assert nothing and pass any graph vacuously", family)
		}
	}
}

// TestEveryWaivedFamilyIsRegistered binds the manifest's waived families to the
// code that must resolve them (#5543).
//
// The waiver rows and the edge-type registries are maintained in different
// places and neither knows about the other. A waived family that does not
// resolve is one whose waiver can never be retired — `assert-edges` errors
// before any Odù or expected-edge set gets a chance — and that is invisible
// until someone spends a live-gate acquisition finding out.
//
// The roster is the bridge: every family carrying a waiver must appear in it,
// so removing a family from the registry while its waiver still stands fails
// here rather than at the gate.
func TestEveryWaivedFamilyIsRegistered(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join(repoRootDir(t), "specs", MaterializedEdgeManifestFileName)
	waivers, err := LoadMaterializedEdgeWaivers(manifest)
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}
	if len(waivers) == 0 {
		// The success state for #5543 is zero waivers. Log rather than fail, or
		// this test would start failing exactly when the epic is finished.
		t.Log("no waivers remain; every family carries real coverage")
		return
	}

	roster := map[string]struct{}{}
	for _, family := range materializedEdgeFamiliesUnderUmbrella {
		roster[family] = struct{}{}
	}

	seen := map[string]struct{}{}
	for _, waiver := range waivers {
		family := strings.TrimPrefix(waiver.Surface, MaterializedEdgeSurfacePrefix)
		if _, already := seen[family]; already {
			continue
		}
		seen[family] = struct{}{}

		if _, ok := roster[family]; !ok {
			t.Errorf("family %q carries a waiver but is not on the umbrella roster; its waiver can never be retired by this work", family)
			continue
		}
		if _, err := MaterializedEdgeDomainEdgeTypes(family); err != nil {
			t.Errorf("waived family %q does not resolve: %v; the waiver cannot be retired because assert-edges errors before any fixture is consulted", family, err)
		}
	}
}

// TestEveryRegisteredEdgeTypeIsCanonical cross-checks every family's types
// against the canonical edgetype registry.
//
// This is the third independent source behind each derivation, after the write
// template and the retract alternation. It catches the failure the other two
// cannot: a typo'd or invented relationship type that is internally consistent
// between a registry and its retract but names an edge the graph never carries.
// Such a type would make the live gate assert a population that is always empty
// — a guard that can never fail.
func TestEveryRegisteredEdgeTypeIsCanonical(t *testing.T) {
	t.Parallel()

	canonical := map[string]struct{}{}
	for _, e := range edgetype.All() {
		canonical[string(e)] = struct{}{}
	}
	if len(canonical) == 0 {
		t.Fatal("edgetype.All() is empty; this test would pass vacuously")
	}

	for _, family := range materializedEdgeFamiliesUnderUmbrella {
		types, err := MaterializedEdgeDomainEdgeTypes(family)
		if err != nil {
			continue // TestEveryUmbrellaFamilyResolves owns that failure
		}
		var unknown []string
		for edgeTypeName := range types {
			if _, ok := canonical[edgeTypeName]; !ok {
				unknown = append(unknown, edgeTypeName)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			t.Errorf("family %q registers %v, which the canonical edgetype registry does not define; the live gate would assert an always-empty population for them",
				family, unknown)
		}
	}
}

// TestWaiverRowsAreOnePerGatePerFamily derives the waiver arithmetic instead of
// writing it down. Every waived family must carry exactly one row per live
// gate, so the row total is always twice the family count.
//
// This exists because the hand-written totals drifted silently and repeatedly.
// A comment here claimed "20 waiver rows" against an actual 18, cmd/ifa's
// README claimed nine coverage rows against an actual 11, and two branches
// each independently took a "not-yet-covered families" count from 11 to 10 --
// producing a conflict-free merge that kept 10 when the truth was 9. Every one
// of those numbers was pinned as a required literal in a documentation guard,
// so the guards cemented the stale values rather than catching them: correcting
// the prose turned them red. A derived invariant cannot go stale, and it fails
// on the actual drift rather than on someone fixing the description of it.
func TestWaiverRowsAreOnePerGatePerFamily(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join(repoRootDir(t), "specs", MaterializedEdgeManifestFileName)
	waivers, err := LoadMaterializedEdgeWaivers(manifest)
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}
	gatesByFamily := make(map[string]map[string]struct{}, len(waivers))
	for _, waiver := range waivers {
		family := waiver.Surface
		if _, seen := gatesByFamily[family]; !seen {
			gatesByFamily[family] = make(map[string]struct{}, 2)
		}
		gatesByFamily[family][waiver.ProofGate] = struct{}{}
	}
	for family, gates := range gatesByFamily {
		if len(gates) != 2 {
			t.Errorf("waived family %q carries %d proof gates, want 2 (one per live gate); a family waived for only one gate is silently unproven on the other",
				family, len(gates))
		}
	}
	if got, want := len(waivers), 2*len(gatesByFamily); got != want {
		t.Errorf("waiver rows = %d, want %d (2 per waived family across %d families)",
			got, want, len(gatesByFamily))
	}
}
