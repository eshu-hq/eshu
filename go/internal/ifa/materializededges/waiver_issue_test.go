// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// materializedEdgeFamilyChildIssue binds each waivable SHARED-projection family
// to the ONE child issue whose closure retires its waivers (#5543's
// decomposition).
//
// The binding is exact on purpose. A rule that merely rejected umbrella issues
// would accept any other number, so repointing repo_dependency's waiver at the
// rationale child would pass review while closing that child retired nothing.
// "Tracked" has to mean tracked by the work that actually removes this row.
//
// A family gaining full coverage drops out of the waivers entirely; it does not
// need removing from this map, and leaving it here documents which child owned
// the gap.
//
// The direct-materialization half is bound separately, by
// directMaterializedEdgeFamilyIssue below, because it is not a decomposition:
// one issue owns all of it.
var materializedEdgeFamilyChildIssue = map[string]string{
	"code_calls":                 "#5991",
	"codeowners_ownership_edges": "#5992",
	"deployable_unit_edges":      "#5993",
	"documentation_edges":        "#5994",
	"handles_route":              "#5995",
	"inheritance_edges":          "#5996",
	"invokes_cloud_action":       "#5997",
	"rationale_edges":            "#5998",
	"repo_dependency":            "#5999",
	"runs_in":                    "#6000",
	"shell_exec":                 "#6001",
	"submodule_pin_edges":        "#6002",
	"workload_dependency":        "#6003",
}

// directMaterializedEdgeFamilyIssue is the issue whose closure retires a
// direct-materialization waiver (#6228).
//
// One issue rather than a per-family map, because #6228 is not an umbrella in
// the sense #5543 was. #5543 was decomposed into thirteen children and then
// closed while thirteen waivers still pointed at it; #6228 stays open until the
// last direct family moves, and its own body says so. Binding all of them to it
// therefore still names work whose completion removes the row — the property
// this whole guard is about — and a per-family map of twenty-eight identical
// values would only invite the same number to be typed twenty-eight ways.
//
// If #6228 is later decomposed the way #5543 was, this constant becomes a map
// on the same day, and the same exactness argument applies to it.
const directMaterializedEdgeFamilyIssue = "#6228"

// materializedEdgeRetiringIssue returns the issue whose closure retires a
// waiver on family, and reports whether the family has such an issue at all.
//
// Both halves of the ledger are resolved here so a waiver cannot be tracked in
// one half's terms while living in the other's file.
func materializedEdgeRetiringIssue(family string, direct map[string]struct{}) (string, bool) {
	if issue, ok := materializedEdgeFamilyChildIssue[family]; ok {
		return issue, true
	}
	if _, ok := direct[family]; ok {
		return directMaterializedEdgeFamilyIssue, true
	}
	return "", false
}

// TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem holds every waiver in
// BOTH ledger halves to naming the specific issue that retires it.
//
// A waiver is a promise that a gap is tracked. Pointed at an umbrella, that
// promise cannot be kept by anyone in particular: #5543 covered all thirteen
// families at once, so no single piece of work closed it and no waiver was ever
// retired by it. Thirteen waivers pointed there and none moved. Pointed at the
// wrong child, it is worse than vague — it is wrong, and it reads as precise.
//
// Both halves, through LoadMaterializedEdgeLedger, because reading one file is
// the blindness #6181 reported one level down: this guard read only the shared
// manifest, so the fifty-six direct waivers it added were held to nothing, and
// repointing one of them at closed #5543 — the exact stale pointer #6181 exists
// to remove — passed. LoadMaterializedEdgeWaivers alone enforces non-blank and
// nothing more.
//
// The manifest already treats a waiver whose surface gains real coverage as
// stale. This is the same discipline one step earlier.
func TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	_, waivers, err := LoadMaterializedEdgeLedger(filepath.Join(repoRoot, "specs"))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeLedger: %v", err)
	}

	direct := setOf(reducer.DirectMaterializedEdgeFamilies())
	if len(direct) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeFamilies() returned zero families; every direct waiver would report as untracked for the wrong reason")
	}
	// The two bindings are resolved in order, so a family in both maps would be
	// silently graded by the shared one. The enumerations are disjoint today and
	// the resolution order only stays honest while they are.
	for family := range direct {
		if issue, clash := materializedEdgeFamilyChildIssue[family]; clash {
			t.Errorf("family %q is enumerated as a direct-materialization family and also bound to %s in materializedEdgeFamilyChildIssue; one family cannot be retired by two different issues, and the shared binding would silently win here",
				family, issue)
		}
	}

	// An empty waiver set is the SUCCESS state, not a failure: it means every
	// family reached real coverage on both gates and the last waiver was removed
	// with it. Failing here would turn the finish line into a red build. The
	// blocking coverage gate is what catches a genuine gap, by requiring every
	// (surface, proof_gate) pair to be covered or waived — so an empty set that
	// is NOT earned fails there, loudly, rather than silently passing here.
	if len(waivers) == 0 {
		t.Log("no waivers remain; every materialized-edge family is covered on both gates")
		return
	}

	// A blank issue is not checked here on purpose: LoadMaterializedEdgeWaivers
	// already refuses to load one ("waiver %q has blank issue"), so a branch for
	// it in this test could never fire. Seeding a blank issue proves that — the
	// failure comes from the loader, at the t.Fatalf above, never from a check
	// down here. An unreachable guard reads like coverage and is not.
	for _, w := range waivers {
		family := strings.TrimPrefix(w.Surface, MaterializedEdgeSurfacePrefix)
		want, known := materializedEdgeRetiringIssue(family, direct)
		if !known {
			t.Errorf("waiver %s/%s names family %q, which is bound to no retiring issue — it is in neither materializedEdgeFamilyChildIssue nor reducer.DirectMaterializedEdgeFamilies(); bind it to the work that retires it rather than leaving the row pointed at nothing in particular",
				w.Surface, w.ProofGate, family)
			continue
		}
		if w.Issue != want {
			t.Errorf("waiver %s/%s names %s, but %s is retired by %s; closing %s would remove nothing here",
				w.Surface, w.ProofGate, w.Issue, family, want, w.Issue)
		}
	}
	t.Logf("checked %d waiver(s) across both ledger halves against their retiring issue", len(waivers))
}
