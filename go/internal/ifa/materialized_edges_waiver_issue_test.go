// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"strings"
	"testing"
)

// materializedEdgeFamilyChildIssue binds each waivable family to the ONE child
// issue whose closure retires its waivers (#5543's decomposition).
//
// The binding is exact on purpose. A rule that merely rejected umbrella issues
// would accept any other number, so repointing repo_dependency's waiver at the
// rationale child would pass review while closing that child retired nothing.
// "Tracked" has to mean tracked by the work that actually removes this row.
//
// A family gaining full coverage drops out of the waivers entirely; it does not
// need removing from this map, and leaving it here documents which child owned
// the gap.
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

// TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem holds every waiver to
// naming the specific child issue that retires it.
//
// A waiver is a promise that a gap is tracked. Pointed at an umbrella, that
// promise cannot be kept by anyone in particular: #5543 covered all thirteen
// families at once, so no single piece of work closed it and no waiver was ever
// retired by it. Thirteen waivers pointed there and none moved. Pointed at the
// wrong child, it is worse than vague — it is wrong, and it reads as precise.
//
// The manifest already treats a waiver whose surface gains real coverage as
// stale. This is the same discipline one step earlier.
func TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	waivers, err := LoadMaterializedEdgeWaivers(filepath.Join(repoRoot, "specs", MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
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
		want, known := materializedEdgeFamilyChildIssue[family]
		if !known {
			t.Errorf("waiver %s/%s names family %q, which has no per-domain child issue in materializedEdgeFamilyChildIssue; add the child that retires it rather than leaving the row pointed at nothing in particular",
				w.Surface, w.ProofGate, family)
			continue
		}
		if w.Issue != want {
			t.Errorf("waiver %s/%s names %s, but %s is retired by %s; closing %s would remove nothing here",
				w.Surface, w.ProofGate, w.Issue, family, want, w.Issue)
		}
	}
	t.Logf("checked %d waiver(s) against their retiring child issue", len(waivers))
}
