// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"testing"
)

// materializedEdgeUmbrellaIssues are tracking issues that will never retire an
// individual family waiver on their own.
//
// #5344 is the epic and #5543 is its materialized-edge umbrella, now decomposed
// into per-domain children (#5991-#6003). #5351 is the gate's original issue.
// A waiver naming any of these says "tracked" without saying who closes it.
var materializedEdgeUmbrellaIssues = map[string]string{
	"#5344": "epic",
	"#5543": "materialized-edge umbrella, decomposed into #5991-#6003",
	"#5351": "the gate's own issue",
}

// TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem holds every waiver to
// naming a per-domain child issue rather than an umbrella.
//
// A waiver is a promise that a gap is tracked. Pointed at an umbrella, that
// promise cannot be kept by anyone in particular: #5543 covered all thirteen
// families at once, so no single piece of work closed it and no waiver was ever
// retired by it. Thirteen waivers pointed there and none moved.
//
// The manifest already treats a waiver whose surface gains real coverage as
// stale. This is the same discipline one step earlier — the waiver has to name
// the issue whose closure removes it, so "tracked" means something a reader can
// act on.
func TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	waivers, err := LoadMaterializedEdgeWaivers(filepath.Join(repoRoot, "specs", MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}
	if len(waivers) == 0 {
		t.Fatal("no waivers loaded; this gate cannot vouch for a set it never read")
	}

	// A blank issue is not checked here on purpose: LoadMaterializedEdgeWaivers
	// already refuses to load one ("waiver %q has blank issue"), so a branch for
	// it in this test could never fire. Seeding a blank issue proves that — the
	// failure comes from the loader, at the t.Fatalf above, never from a check
	// down here. An unreachable guard reads like coverage and is not.
	for _, w := range waivers {
		if why, isUmbrella := materializedEdgeUmbrellaIssues[w.Issue]; isUmbrella {
			t.Errorf("waiver %s/%s names %s (%s), which retires no single family; name the per-domain child issue that closing removes this waiver",
				w.Surface, w.ProofGate, w.Issue, why)
		}
	}
	t.Logf("checked %d waiver(s) for a retiring issue", len(waivers))
}
