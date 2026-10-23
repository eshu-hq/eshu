// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// materializedEdgeFamilyTriggerStems maps a materialized-edge family to the
// substring its own gate triggers carry. It is a hand-maintained map on
// purpose: a family's trigger paths follow no derivable rule (code_calls owns
// `code_call*`, sql_relationships owns `sql_relationship*`, and neither is the
// family key), so guessing the stem from the family name would produce a
// check that silently matches nothing.
//
// A WRONG stem is loud -- the family's own coverage row goes red the moment it
// lands. A MISSING entry is silent, which is why
// TestEveryCoveredFamilyTriggersBothLiveGates asserts this map's key set
// against the family enumerations in both directions, mirroring
// TestMaterializedEdgeIdentityByFamilyIsTotal.
//
// Stems for the not-yet-covered SHARED families are prospective: nothing checks
// them until that family gets a coverage row, which is exactly the point --
// declaring them now means the family that lands coverage inherits a working
// check instead of writing one under time pressure.
//
// The DIRECT families (#6181) are deliberately absent until each one claims a
// coverage row. Their live wiring does not exist yet -- #6228 owns writing it --
// so a stem declared now would be a guess at trigger paths nobody has written,
// asserted by a guard whose whole subject is not asserting unverified things.
// The check below demands the entry at the moment the row lands instead, which
// is the same obligation arriving when it can actually be met.
//
// TWO EXCEPTIONS, and they are exceptions to the REASON rather than to the
// rule: iam_instance_profile_role and kubernetes_namespace_environment carried
// stems while still waived. #6228 wired both into the live determinism matrix,
// so their ifa-determinism triggers were written files, not guesses, and the
// stems below were read off them. #6309 then wired the fault half and converted
// both waivers to coverage rows, so both families are now covered and the
// coverage-keyed checks hold them the same as every other covered family. Do
// not re-remove them by applying the paragraph above: a stem whose triggers
// exist is the thing that paragraph is waiting for. iam_can_perform joined
// them the same way: its determinism triggers were written files since the
// iam-can-perform live-cell change, and the #6228 fault half wired the fault
// side and converted its waivers to coverage rows in the same change.
var materializedEdgeFamilyTriggerStems = map[string]string{
	"code_calls":                 "code_call",
	"codeowners_ownership_edges": "codeowners",
	"deployable_unit_edges":      "deployable_unit",
	"documentation_edges":        "documentation",
	// handles_route/runs_in/invokes_cloud_action (#5995/#6000/#5997) are the
	// one exception to "stem is a prefix of the family's own name": all three
	// share ONE cassette and ONE handler-side extraction seam, so their real
	// ci-gates.v1.yaml triggers are wired under the shared "symbol_runtime"
	// name (e.g. "go/internal/ifa/symbol_runtime_family_odu.go",
	// "testdata/cassettes/symbolruntime/**"), not under any of the three
	// family names individually. A stem of "handles_route" (etc.) matched
	// nothing in either gate block and went unnoticed while these rows were
	// still waived -- exactly the WRONG-STEM-goes-loud-only-once-covered case
	// this map's own doc comment above describes; it went red the moment
	// their coverage rows landed.
	"handles_route":        "symbol_runtime",
	"inheritance_edges":    "inheritance",
	"invokes_cloud_action": "symbol_runtime",
	"rationale_edges":      "rationale",
	"repo_dependency":      "repo_dependency",
	"runs_in":              "symbol_runtime",
	"shell_exec":           "shell_exec",
	"sql_relationships":    "sql_relationship",
	"submodule_pin_edges":  "submodule",
	"workload_dependency":  "workload_dependency",
	// The first two DIRECT families to be wired into a live gate (#6228). Both
	// stems are prefixes of real trigger paths in the ifa-determinism block --
	// go/internal/ifa/<stem>_family_odu.go, the reducer handler and the cypher
	// writer -- rather than guesses at paths nobody has written, which is the
	// state the comment above describes for the remaining direct families.
	//
	// In the ifa-fault-injection block since #6309: all nine families have
	// fault cells asserting their exact edge sets, so their inputs retrigger
	// that gate. The both-gates check below is keyed to coverage rows, and
	// all nine families are covered, so it demands the fault-side trigger --
	// and finds it, in the per-family cell files and the shared fault seams.
	//
	// iam_can_assume, iam_can_perform, ec2_uses_profile, s3_logs_to,
	// iam_escalation, and authenticates_vault_role join them with
	// the #6228 fault halves and slice A: same stem shape, read off the
	// fault cells files and the cypher writers the triggers name.
	"iam_can_assume":                   "iam_can_assume",
	"ec2_uses_profile":                 "ec2_uses_profile",
	"iam_instance_profile_role":        "iam_instance_profile_role",
	"kubernetes_namespace_environment": "kubernetes_namespace_environment",
	// The fourth DIRECT family wired into a live gate (#6228 fault half).
	// Its ifa-determinism triggers were already written files (family Odù,
	// cassettes, expected-edge set); the fault half adds the writer, the
	// per-family cell files, and the shared fault seams, so the stem below
	// is read off them, not guessed.
	"iam_can_perform": "iam_can_perform",
	// s3_logs_to joins them with the #6228 fault half: same stem shape,
	// read off the fault cells file and the cypher writer the triggers name.
	"s3_logs_to": "s3_logs_to",
	// iam_escalation joins them with the #6228 fault half: same stem shape,
	// read off the fault cells file and the cypher writer the triggers name.
	"iam_escalation": "iam_escalation",
	// authenticates_vault_role joins them with the #6228 slice A full
	// build: same stem shape, read off the fault cells file and the cypher
	// writer the triggers name.
	"authenticates_vault_role": "authenticates_vault_role",
	// uses_vault_policy joins them with the #6228 slice A full
	// build: same stem shape, read off the fault cells file and the cypher
	// writer the triggers name.
	"uses_vault_policy": "uses_vault_policy",
	// built_from joins them with the #6228 slice C live leaf: same stem
	// shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_built_from_cells.sh), the fault
	// cases file, and the Odù the triggers name.
	"built_from": "built_from",
	// cloud_resource_container_image joins them with the #6228 slice D
	// live leaf: same stem shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh),
	// the fault cases file, and the Odù the triggers name.
	"cloud_resource_container_image": "cloud_resource_container_image",
	// crossplane_satisfied_by joins them with the #6228 crossplane live
	// leaf: same stem shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh),
	// the fault cases file, and the Odù the triggers name.
	"crossplane_satisfied_by": "crossplane_satisfied_by",
	// derived_from joins them with the #6228 derived_from live leaf: same
	// stem shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_derived_from_cells.sh), the fault
	// cases file, and the Odù the triggers name.
	"derived_from": "derived_from",
	// code_interproc_evidence joins them with the #6228 code_interproc
	// live leaf: same stem shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh),
	// the fault cases file, and the Odù the triggers name.
	"code_interproc_evidence": "code_interproc_evidence",
	// code_taint_evidence joins them with the #6228 code_taint_evidence
	// live leaf: same stem shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh),
	// the fault cases file, and the Odù the triggers name.
	"code_taint_evidence": "code_taint_evidence",
	// observability_coverage joins them with the #6228 observability_coverage
	// live leaf: same stem shape, read off the fault cells file
	// (scripts/lib/ifa_fault_injection_observability_coverage_cells.sh),
	// the fault cases file, and the Odù the triggers name.
	"observability_coverage": "observability_coverage",
}

// TestEveryCoveredFamilyTriggersBothLiveGates closes the third side of the
// triangle. Two checks already exist: the registry-derived loop in
// scripts/test-verify-ifa-determinism.sh proves registry ⊆ workflow, and the
// selector table proves the concrete paths select both gates. Neither notices
// a family that declares NO triggers at all -- both iterate what the registry
// already names, so an unwired family is not merely unchecked, it is
// vacuously green.
//
// A coverage row asserts the live matrices proved that family. A family with
// no trigger in a gate never re-runs that gate when its own Odù, cassette,
// extractor, or writer changes, so the row keeps asserting a proof that has
// gone stale. This check binds the two.
//
// It reads the MERGED ledger and BOTH family enumerations (#6181). Reading the
// shared manifest and reducer.MaterializedEdgeFamilies() alone left the direct
// half outside the check entirely: when #6228 replaces a direct family's waiver
// with a real coverage row, the combined reconciliation accepts that row while
// this guard never looks at it, so the family could claim a live proof with
// neither gate retriggered by its fixture or its writer. Measured, not assumed:
// a seeded direct coverage row for iam_can_perform passed this test unchanged
// before the merge and fails after it.
//
// It is keyed to COVERAGE ROWS, not to all families, and deliberately so.
// Requiring triggers of every family would land a red row for every family
// that is honestly waived and not yet wired, and a check that ships red is a
// check somebody switches off. Keyed to coverage it lands clean and stays purely
// prospective: the next family to claim a row has to wire its triggers in the
// same change.
//
// What it does NOT cover: whether the triggers a covered family declares are
// the RIGHT ones. Nothing checks that, and nothing plausibly can -- a gate
// cannot know which files feed a family's proof. That remains a review
// responsibility.
func TestEveryCoveredFamilyTriggersBothLiveGates(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	shared := reducer.MaterializedEdgeFamilies()
	if len(shared) == 0 {
		t.Fatal("reducer.MaterializedEdgeFamilies() returned zero families; every check below would pass vacuously")
	}
	direct := reducer.DirectMaterializedEdgeFamilies()
	if len(direct) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeFamilies() returned zero families; the direct half would silently drop out of every check below")
	}
	families := make([]string, 0, len(shared)+len(direct))
	families = append(families, shared...)
	families = append(families, direct...)

	// The merged ledger, not one half of it. LoadMaterializedEdgeLedger is what
	// the reconciliation itself reads, so a direct coverage row is visible here
	// on exactly the terms it is accepted there.
	manifest, _, err := LoadMaterializedEdgeLedger(specsDir)
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeLedger: %v", err)
	}
	covered := map[string]struct{}{}
	for _, row := range manifest.Coverage {
		covered[strings.TrimPrefix(row.Surface, MaterializedEdgeSurfacePrefix)] = struct{}{}
	}
	if len(covered) == 0 {
		t.Fatal("no materialized-edge family carries a coverage row; this check would pass vacuously")
	}

	// Blank is rejected alongside missing. `"new_family": ""` is the natural
	// placeholder and it satisfies key-set equality, so checking presence
	// alone would let the map stay total while the value it contributes
	// matches every trigger vacuously -- the exact silence this half of the
	// guard exists to break.
	for _, family := range shared {
		if stem, ok := materializedEdgeFamilyTriggerStems[family]; !ok || strings.TrimSpace(stem) == "" {
			t.Errorf("shared family %q has no usable trigger stem (present=%t, stem=%q); give it a non-blank entry in materializedEdgeFamilyTriggerStems or this family's coverage row would be checked against nothing", family, ok, stem)
		}
	}
	// The direct half owes a stem the moment it claims a coverage row, and not
	// before. Demanding one from all twenty-eight today would mean inventing
	// stems for trigger paths #6228 has not written yet; demanding one from a
	// family that claims live proof is the same obligation, arriving when it
	// can be met from triggers that exist.
	for _, family := range direct {
		if _, isCovered := covered[family]; !isCovered {
			continue
		}
		if stem, ok := materializedEdgeFamilyTriggerStems[family]; !ok || strings.TrimSpace(stem) == "" {
			t.Errorf("direct family %q claims a coverage row but has no usable trigger stem (present=%t, stem=%q); a direct family's live wiring lands with its coverage row, so give it a non-blank entry in materializedEdgeFamilyTriggerStems in the same change or the row is checked against nothing", family, ok, stem)
		}
	}
	for family := range materializedEdgeFamilyTriggerStems {
		if !slices.Contains(families, family) {
			t.Errorf("trigger stem declared for %q, which neither reducer.MaterializedEdgeFamilies() nor reducer.DirectMaterializedEdgeFamilies() enumerates; remove the stale entry", family)
		}
	}

	proofGates, err := cigates.Load(filepath.Join(specsDir, "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}
	triggersByGate := map[string][]string{}
	for _, gateID := range []string{materializedEdgeProofGateBaseline, materializedEdgeProofGateFault} {
		var found bool
		for i := range proofGates.Gates {
			if proofGates.Gates[i].ID == gateID {
				triggersByGate[gateID] = proofGates.Gates[i].Triggers
				found = true
			}
		}
		if !found {
			t.Fatalf("%s gate not found in ci-gates registry", gateID)
		}
	}

	for _, family := range families {
		if _, ok := covered[family]; !ok {
			continue
		}
		stem := strings.TrimSpace(materializedEdgeFamilyTriggerStems[family])
		if stem == "" {
			// Missing or blank, both already reported by the totality loops
			// above. Continuing here would otherwise match every trigger and
			// report the family as wired.
			continue
		}
		for gateID, triggers := range triggersByGate {
			matched := 0
			for _, trigger := range triggers {
				if strings.Contains(trigger, stem) {
					matched++
				}
			}
			if matched == 0 {
				t.Errorf("family %q claims a coverage row but the %s gate declares no trigger containing %q; that gate never re-runs when the family's own inputs change, so the row asserts a proof nobody refreshes", family, gateID, stem)
			}
		}
	}
}
