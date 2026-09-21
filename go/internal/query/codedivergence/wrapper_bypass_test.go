// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"strings"
	"testing"
)

// wrapperBypassMembers builds a same-name thin family: five 12-token
// recordAPICall-shaped wrappers across packages. Twelve tokens sit below
// the 50-token floor on purpose: thinness is definitional for wrappers,
// so the floor must not apply to this kind.
func wrapperBypassMembers() []Member {
	packages := []string{"svc-a", "svc-b", "svc-c", "svc-d", "svc-e"}
	members := make([]Member, 0, len(packages))
	for i, pkg := range packages {
		members = append(members, Member{
			EntityID:     "wrapper-entity-" + string(rune('a'+i)),
			EntityName:   "recordAPICall",
			EntityType:   "Function",
			RelativePath: pkg + "/telemetry.go",
			Language:     "Go",
			StartLine:    10,
			EndLine:      16,
			TokenCount:   12,
		})
	}
	return members
}

func qualifiedSelection() BypassSelection {
	return BypassSelection{
		CanonicalID:   "wrapper-entity-a",
		CanonicalName: "recordAPICall",
		TargetID:      "target-entity-impl",
		TargetName:    "recordAPICallImpl",
		Confidence:    0.82,
		Qualified:     true,
	}
}

func sumReasonValues(reasons []Reason) int {
	sum := 0
	for _, reason := range reasons {
		sum += reason.Value
	}
	return sum
}

func TestAssembleWrapperBypassFindingPositive(t *testing.T) {
	finding, ok := AssembleWrapperBypassFinding("repo-1", "target-entity-impl", wrapperBypassMembers(), false, qualifiedSelection())
	if !ok {
		t.Fatalf("AssembleWrapperBypassFinding = not ok, suppressions %v", finding.Suppressions)
	}
	if finding.Kind != KindWrapperBypass {
		t.Errorf("Kind = %q, want %q", finding.Kind, KindWrapperBypass)
	}
	if len(finding.Members) != 5 {
		t.Errorf("Members = %d, want 5 (floor must not apply)", len(finding.Members))
	}
	if finding.Score != 5*12 {
		t.Errorf("Score = %d, want %d", finding.Score, 5*12)
	}
	if got := sumReasonValues(finding.Reasons); got != finding.Score {
		t.Errorf("reasons sum = %d, want score %d", got, finding.Score)
	}
	if finding.Confidence != 0.82 {
		t.Errorf("Confidence = %v, want 0.82 (weakest edge)", finding.Confidence)
	}
	if finding.Fingerprint != "target-entity-impl" {
		t.Errorf("Fingerprint = %q, want the target entity id", finding.Fingerprint)
	}
}

func TestAssembleWrapperBypassFindingMixedNamesAssemble(t *testing.T) {
	// Members mix the wrapper and bypasser names by construction: the
	// family shape gate lives in WrapperFamilySurvivors (the caller),
	// never in Assemble.
	members := wrapperBypassMembers()[:2]
	members[1].EntityID = "bypasser-entity-x"
	members[1].EntityName = "otherPackageCaller"
	members[1].RelativePath = "svc-x/client.go"
	sel := qualifiedSelection()
	finding, ok := AssembleWrapperBypassFinding("repo-1", "target-entity-impl", members, false, sel)
	if !ok {
		t.Fatalf("AssembleWrapperBypassFinding = not ok for mixed names, suppressions %v", finding.Suppressions)
	}
	if finding.Members[0].EntityID != "wrapper-entity-a" {
		t.Errorf("first member = %q, want canonical first", finding.Members[0].EntityID)
	}
}

func TestAssembleWrapperBypassFindingSuppressesTestMembers(t *testing.T) {
	// Catalogue minus floor: a test-file bypasser suppresses while thin
	// (12-token) members survive.
	members := wrapperBypassMembers()[:3]
	members[2].RelativePath = "svc-c/telemetry_test.go"
	finding, ok := AssembleWrapperBypassFinding("repo-1", "target-entity-impl", members, false, qualifiedSelection())
	if !ok {
		t.Fatalf("AssembleWrapperBypassFinding = not ok, suppressions %v", finding.Suppressions)
	}
	if len(finding.Members) != 2 {
		t.Errorf("Members = %d, want 2 (test file suppressed)", len(finding.Members))
	}
	if finding.Suppressions[RuleTestFile] != 1 {
		t.Errorf("suppressions[test_file] = %d, want 1", finding.Suppressions[RuleTestFile])
	}
}

func TestAssembleWrapperBypassFindingUnqualified(t *testing.T) {
	sel := qualifiedSelection()
	sel.Qualified = false
	finding, ok := AssembleWrapperBypassFinding("repo-1", "target-entity-impl", wrapperBypassMembers(), false, sel)
	if ok {
		t.Fatalf("AssembleWrapperBypassFinding = ok for negative selection")
	}
	if finding.Suppressions[RuleWrapperUnqualified] != 5 {
		t.Errorf("suppressions[wrapper_not_qualified] = %d, want 5", finding.Suppressions[RuleWrapperUnqualified])
	}
}

func TestAssembleWrapperBypassFindingAmbiguousInferred(t *testing.T) {
	sel := qualifiedSelection()
	sel.Ambiguous = true
	sel.AmbiguityNote = "canonical edge rests on unique-name inference"
	finding, ok := AssembleWrapperBypassFinding("repo-1", "target-entity-impl", wrapperBypassMembers(), false, sel)
	if !ok {
		t.Fatalf("AssembleWrapperBypassFinding = not ok for inferred-edge admission, suppressions %v", finding.Suppressions)
	}
	found := false
	for _, reason := range finding.Reasons {
		if reason.Code == ReasonCanonicalAmbiguous {
			found = true
			if reason.Value != 0 {
				t.Errorf("ambiguous reason value = %d, want 0 (signal only)", reason.Value)
			}
			if !strings.Contains(reason.Sentence, "unique-name inference") {
				t.Errorf("ambiguous sentence %q misses the note", reason.Sentence)
			}
		}
	}
	if !found {
		t.Errorf("no canonical_ambiguous reason in %v", finding.Reasons)
	}
	if got := sumReasonValues(finding.Reasons); got != finding.Score {
		t.Errorf("reasons sum = %d, want score %d", got, finding.Score)
	}
}

func TestWrapperFamilySurvivorsGate(t *testing.T) {
	survivors, _, shaped := WrapperFamilySurvivors(wrapperBypassMembers(), false)
	if !shaped {
		t.Fatalf("WrapperFamilySurvivors = not shaped for same-name family")
	}
	if len(survivors) != 5 {
		t.Errorf("survivors = %d, want 5", len(survivors))
	}
	mixed := wrapperBypassMembers()
	mixed[0].EntityName = "other"
	if _, _, shaped := WrapperFamilySurvivors(mixed, false); shaped {
		t.Errorf("WrapperFamilySurvivors = shaped for mixed-name group")
	}
}
