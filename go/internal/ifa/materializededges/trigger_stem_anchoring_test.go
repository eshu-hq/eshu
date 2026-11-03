// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestTriggerStemAnchoringRejectsSiblingTriggers is the seeded-violation half
// of #9867: every cloud_resource_container_image trigger path contains the
// cloud_resource stem, so a bare substring match let sibling-only wiring
// satisfy the cloud_resource row. The anchored match must reject those while
// keeping the family's own Odu, cells, live-drive, and writer paths matched.
// All trigger literals below are pinned from the live registry, not invented:
// renaming a registry path without updating this test fails it by design.
func TestTriggerStemAnchoringRejectsSiblingTriggers(t *testing.T) {
	t.Parallel()

	stemSet := make(map[string]struct{}, len(materializedEdgeFamilyTriggerStems))
	for _, s := range materializedEdgeFamilyTriggerStems {
		if strings.TrimSpace(s) == "" {
			continue
		}
		stemSet[s] = struct{}{}
	}

	siblingTriggers := []string{
		"go/internal/ifa/familyodu/cloud_resource_container_image_family_odu.go",
		"scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh",
	}
	for _, trigger := range siblingTriggers {
		if triggerMatchesFamilyStem(trigger, "cloud_resource", stemSet) {
			t.Errorf("sibling trigger %q satisfies stem %q; a cloud_resource row wired only to its sibling would pass the guard", trigger, "cloud_resource")
		}
		if !triggerMatchesFamilyStem(trigger, "cloud_resource_container_image", stemSet) {
			t.Errorf("sibling trigger %q does not satisfy its own stem %q; anchoring must attribute it to the sibling, not drop it", trigger, "cloud_resource_container_image")
		}
	}

	ownTriggers := []string{
		"go/internal/ifa/familyodu/cloud_resource_family_odu.go",
		"scripts/lib/ifa_fault_injection_cloud_resource_cells.sh",
		"scripts/lib/ifa_direct_family_live_cloud_resource.sh",
		"go/internal/storage/cypher/cloud/gcp/relationship/gcp_cloud_resource_edge_writer.go",
		"go/internal/storage/cypher/cloud/azure/relationship/azure_cloud_resource_edge_writer.go",
	}
	for _, trigger := range ownTriggers {
		if !triggerMatchesFamilyStem(trigger, "cloud_resource", stemSet) {
			t.Errorf("own trigger %q no longer satisfies stem %q; anchoring regressed genuine wiring", trigger, "cloud_resource")
		}
	}

	// The shared-stem exception still holds: three families share one
	// cassette and one handler-side seam under the symbol_runtime name.
	sharedTriggers := []string{
		"go/internal/ifa/familyodu/symbol_runtime_family_odu.go",
		"scripts/lib/ifa_fault_injection_inheritance_cells.sh",
	}
	if !triggerMatchesFamilyStem(sharedTriggers[0], "symbol_runtime", stemSet) {
		t.Errorf("shared trigger %q no longer satisfies stem %q", sharedTriggers[0], "symbol_runtime")
	}
	if triggerMatchesFamilyStem(sharedTriggers[1], "symbol_runtime", stemSet) {
		t.Errorf("unrelated trigger %q satisfies stem %q; anchoring must not widen matches", sharedTriggers[1], "symbol_runtime")
	}
}

// TestTriggerStemLongestWins pins the anchoring rule on synthetic stems: the
// longest stem covering an occurrence wins, and an uncovered occurrence still
// matches even when a longer stem appears elsewhere in the same path.
func TestTriggerStemLongestWins(t *testing.T) {
	t.Parallel()

	stems := map[string]struct{}{"stem": {}, "stem_leaf": {}}

	if triggerMatchesFamilyStem("pkg/stem_leaf_cells.sh", "stem", stems) {
		t.Error(`"pkg/stem_leaf_cells.sh" satisfies "stem"; the longer "stem_leaf" covers that occurrence`)
	}
	if !triggerMatchesFamilyStem("pkg/stem_leaf_cells.sh", "stem_leaf", stems) {
		t.Error(`"pkg/stem_leaf_cells.sh" does not satisfy "stem_leaf"`)
	}
	if !triggerMatchesFamilyStem("pkg/stem_cells.sh", "stem", stems) {
		t.Error(`"pkg/stem_cells.sh" does not satisfy "stem"`)
	}
	if !triggerMatchesFamilyStem("pkg/stem_other/stem_leaf_cells.sh", "stem", stems) {
		t.Error(`"pkg/stem_other/stem_leaf_cells.sh" does not satisfy "stem" via its uncovered first occurrence`)
	}
	if triggerMatchesFamilyStem("pkg/other.sh", "stem", stems) {
		t.Error(`"pkg/other.sh" satisfies "stem" without containing it`)
	}
	if triggerMatchesFamilyStem("pkg/stem_cells.sh", "", stems) {
		t.Error("blank stem matches; blank must never match")
	}
}

// TestCloudResourceKeepsGenuineWiring ties the seeded pair to the live
// registry: cloud_resource must hold at least one anchored trigger per gate
// that is NOT a sibling path, proving today's passes rest on genuine wiring
// rather than on the sibling the anchor now excludes.
func TestCloudResourceKeepsGenuineWiring(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	proofGates, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}
	stemSet := make(map[string]struct{}, len(materializedEdgeFamilyTriggerStems))
	for _, s := range materializedEdgeFamilyTriggerStems {
		if strings.TrimSpace(s) == "" {
			continue
		}
		stemSet[s] = struct{}{}
	}
	for _, gateID := range []string{materializedEdgeProofGateBaseline, materializedEdgeProofGateFault} {
		var triggers []string
		for i := range proofGates.Gates {
			if proofGates.Gates[i].ID == gateID {
				triggers = proofGates.Gates[i].Triggers
			}
		}
		if len(triggers) == 0 {
			t.Fatalf("%s gate declares no triggers", gateID)
		}
		genuine := 0
		for _, trigger := range triggers {
			if triggerMatchesFamilyStem(trigger, "cloud_resource", stemSet) {
				genuine++
			}
		}
		if genuine == 0 {
			t.Errorf("stem %q has no anchored trigger in gate %s; the anchor removed genuine wiring, not just the sibling", "cloud_resource", gateID)
		}
	}
}
