// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// checkStatementStageCoherence proves the per-statement hold seam's registry
// contract (#8539, arbiter ruling 2026-10-21, seam (2)):
//
//   - blocker_kind=statement_hold MUST pair with wait_stage=statement, and
//     wait_stage=statement MUST pair with blocker_kind=statement_hold. A
//     statement hold engages one graph write, not a queue the handler shares,
//     so a handler- or runner-stage wait on that row would poll a queue the
//     hold never blocks on -- and a non-hold blocker on a statement-stage row
//     would hold nothing the wait rendezvouses with.
//   - A statement-stage wait_key MUST be a real reducer Domain (the same
//     fact_work_items.domain keyspace as the handler stage): the kill cell
//     rendezvouses on the claimed projection work item, then on the hold
//     marker. A key no claim queue can contain would wait on a row that can
//     never appear for this family's write.
//   - Statement-stage exclusivity is judged on the family-own IFA_FAMILY_
//     ANCHOR, not the wait_key: several statement holds may rendezvous on the
//     same projection domain (the handler is shared), but a blocker that
//     engages a write only IT performs must name its OWN edge-type MERGE.
//     Two statement-stage families sharing one anchor means one family's hold
//     would engage the other's write (or vice versa, non-deterministically),
//     and a statement_hold row naming another family's anchor borrows a write
//     it does not own -- both are the template-copy defect, caught here before
//     it burns a live fault shard.
//
// Factored into a pure function over the four parsed registry tables so
// synthetic violating inputs prove each rule fires independent of whether the
// live registry ever actually violates it.
func checkStatementStageCoherence(
	blockerKinds, waitStages, waitKeys, anchors map[string]string,
) error {
	families := make([]string, 0, len(waitStages)+len(blockerKinds))
	seen := make(map[string]struct{})
	for family := range waitStages {
		if _, ok := seen[family]; !ok {
			seen[family] = struct{}{}
			families = append(families, family)
		}
	}
	for family := range blockerKinds {
		if _, ok := seen[family]; !ok {
			seen[family] = struct{}{}
			families = append(families, family)
		}
	}
	sort.Strings(families)

	for _, family := range families {
		blocker := blockerKinds[family]
		stage, hasStage := waitStages[family]
		switch {
		case blocker == string(blockerStatementHold) && (!hasStage || stage != "statement"):
			return fmt.Errorf("family %q declares blocker_kind=statement_hold with wait_stage=%q -- a statement hold engages one graph write, so its wait must rendezvous on the claimed projection work item (wait_stage=statement); a handler- or runner-stage wait polls a queue the hold never blocks on", family, stage)
		case hasStage && stage == "statement" && blocker != string(blockerStatementHold):
			return fmt.Errorf("family %q declares wait_stage=statement with blocker_kind=%q -- a statement-stage wait rendezvouses with a per-statement hold, so only blocker_kind=statement_hold may use it", family, blocker)
		}
	}

	// Exclusivity on the family-own anchor: no two statement-stage families
	// may share one, and a statement_hold anchor must not equal ANY other
	// family's anchor (borrowing a write the family does not own).
	anchorOwner := make(map[string]string)
	anchorFamilies := make([]string, 0, len(anchors))
	for family := range anchors {
		anchorFamilies = append(anchorFamilies, family)
	}
	sort.Strings(anchorFamilies)
	for _, family := range anchorFamilies {
		anchor := anchors[family]
		if anchor == "" {
			continue
		}
		if prior, clash := anchorOwner[anchor]; clash {
			if waitStages[family] == "statement" || waitStages[prior] == "statement" {
				return fmt.Errorf("families %q and %q both declare IFA_FAMILY_ANCHOR=%q and at least one is wait_stage=statement -- one statement hold would engage the other's write and both cells would pass while proving nothing about the family whose scope they claim; each statement-stage family needs its own edge-type MERGE", prior, family, anchor)
			}
			continue
		}
		anchorOwner[anchor] = family
	}
	for _, family := range families {
		if blockerKinds[family] != string(blockerStatementHold) {
			continue
		}
		if anchors[family] == "" {
			return fmt.Errorf("family %q declares blocker_kind=statement_hold with no IFA_FAMILY_ANCHOR -- the hold engages its family's own edge-type MERGE, so a missing anchor is a hold on nothing", family)
		}
	}
	return nil
}

// TestCheckStatementStageCoherence_SeededViolations proves each rule above
// fires on a synthetic violating input, independent of the live registry.
func TestCheckStatementStageCoherence_SeededViolations(t *testing.T) {
	t.Parallel()

	const anchorUses = "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
	const anchorAuth = "MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)"

	tests := map[string]struct {
		blockers map[string]string
		stages   map[string]string
		keys     map[string]string
		anchors  map[string]string
		wantErr  string
	}{
		"statement_hold with handler stage": {
			blockers: map[string]string{"uses_vault_policy": "statement_hold"},
			stages:   map[string]string{"uses_vault_policy": "handler"},
			keys:     map[string]string{"uses_vault_policy": "secrets_iam_graph_projection"},
			anchors:  map[string]string{"uses_vault_policy": anchorUses},
			wantErr:  "wait_stage",
		},
		"statement_hold with no stage": {
			blockers: map[string]string{"uses_vault_policy": "statement_hold"},
			stages:   map[string]string{},
			keys:     map[string]string{},
			anchors:  map[string]string{"uses_vault_policy": anchorUses},
			wantErr:  "wait_stage",
		},
		"statement stage with table_lock blocker": {
			blockers: map[string]string{"uses_vault_policy": "table_lock:fact_records"},
			stages:   map[string]string{"uses_vault_policy": "statement"},
			keys:     map[string]string{"uses_vault_policy": "secrets_iam_graph_projection"},
			anchors:  map[string]string{"uses_vault_policy": anchorUses},
			wantErr:  "blocker_kind=statement_hold",
		},
		"two statement families sharing one anchor": {
			blockers: map[string]string{"family_a": "statement_hold", "family_b": "statement_hold"},
			stages:   map[string]string{"family_a": "statement", "family_b": "statement"},
			keys:     map[string]string{"family_a": "proj_a", "family_b": "proj_b"},
			anchors:  map[string]string{"family_a": anchorUses, "family_b": anchorUses},
			wantErr:  "family_a",
		},
		"statement_hold borrowing another family's anchor": {
			blockers: map[string]string{"uses_vault_policy": "statement_hold", "authenticates_vault_role": "table_lock:fact_records"},
			stages:   map[string]string{"uses_vault_policy": "statement", "authenticates_vault_role": "handler"},
			keys:     map[string]string{"uses_vault_policy": "secrets_iam_graph_projection", "authenticates_vault_role": "secrets_iam_graph_projection"},
			anchors:  map[string]string{"uses_vault_policy": anchorAuth, "authenticates_vault_role": anchorAuth},
			wantErr:  "authenticates_vault_role",
		},
		"statement_hold with no anchor": {
			blockers: map[string]string{"uses_vault_policy": "statement_hold"},
			stages:   map[string]string{"uses_vault_policy": "statement"},
			keys:     map[string]string{"uses_vault_policy": "secrets_iam_graph_projection"},
			anchors:  map[string]string{},
			wantErr:  "no IFA_FAMILY_ANCHOR",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := checkStatementStageCoherence(tc.blockers, tc.stages, tc.keys, tc.anchors)
			if err == nil {
				t.Fatalf("checkStatementStageCoherence() = nil error, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("checkStatementStageCoherence() = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestMaterializedEdgeFamilyBlockerKindsAreKnown proves every registry row's
// blocker_kind is one of the six shapes classifyBlockerKind understands --
// never a typo or a new vocabulary the Go side does not know about. The
// lockstep test only classifies its eight covered families; uses_vault_policy
// (the statement-hold family) is not one of them, so without this test an
// unknown blocker on that row would be checked by nothing.
func TestMaterializedEdgeFamilyBlockerKindsAreKnown(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	declared := parseIfaFamilyRegistryBlockerKinds(t, rowsDir)
	if len(declared) == 0 {
		t.Fatal("parsed zero IFA_FAMILY_BLOCKER_KIND rows -- registry format changed or the rows were emptied")
	}
	for family, raw := range declared {
		family, raw := family, raw
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			if _, known := classifyBlockerKind(raw); !known {
				t.Fatalf("family %q declares blocker kind %q, which is not one of shared_intent_lock, runner_lease_hold, ack_barrier, statement_hold, none, or table_lock:<name> -- the registry's vocabulary drifted from what this classifier understands", family, raw)
			}
		})
	}
}

// TestClassifyBlockerKindCatchesUnknown is the deliberate-break tooth for the
// classifier itself: unknown blockers must fail loudly, and statement_hold
// takes no :<name> suffix (the hold identifies its write through the row's
// IFA_FAMILY_ANCHOR, not through a suffix on the blocker).
func TestClassifyBlockerKindCatchesUnknown(t *testing.T) {
	t.Parallel()

	if _, known := classifyBlockerKind("freeze_ray"); known {
		t.Fatal("classifyBlockerKind(\"freeze_ray\") = known, want unknown: a typo'd blocker must fail loudly, not pass vacuously")
	}
	if _, known := classifyBlockerKind("statement_hold:extra"); known {
		t.Fatal("classifyBlockerKind(\"statement_hold:extra\") = known, want unknown: statement_hold takes no :<name> suffix")
	}
}

// TestIfaFamilyRegistryStatementStageCohereLive runs
// checkStatementStageCoherence against the real, live-parsed registry rows.
// It asserts a non-zero statement-stage count (uses_vault_policy, #8539) for
// the same reason the runner exclusivity test asserts its floor of 3: if the
// WAIT_STAGE parse regex went stale and matched nothing, the check would be
// handed empty maps, return nil, and stay green while proving nothing.
func TestIfaFamilyRegistryStatementStageCohereLive(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	blockerKinds := parseIfaFamilyRegistryBlockerKinds(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	anchors := parseIfaFamilyRegistryAnchors(t, rowsDir)

	statementFamilies := 0
	for _, stage := range waitStages {
		if stage == "statement" {
			statementFamilies++
		}
	}
	if statementFamilies < 1 {
		t.Fatalf("parsed %d wait_stage=statement row(s), want >= 1 (uses_vault_policy, #8539); a lower count means the registry parse returned nothing and the coherence check below would pass vacuously", statementFamilies)
	}
	for family, stage := range waitStages {
		if stage != "statement" {
			continue
		}
		raw, ok := waitKeys[family]
		if !ok || strings.TrimSpace(raw) == "" {
			t.Fatalf("family %q declares wait_stage=statement but no IFA_FAMILY_WAIT_KEY -- a statement-stage wait with no key polls nothing", family)
		}
		if err := reducercontract.Domain(raw).Validate(); err != nil {
			t.Fatalf("family %q: wait_stage=statement but wait_key %q is not a real reducer Domain (%v) -- the kill cell rendezvouses on the claimed projection work item in fact_work_items", family, raw, err)
		}
	}

	if err := checkStatementStageCoherence(blockerKinds, waitStages, waitKeys, anchors); err != nil {
		t.Fatal(err)
	}
}
