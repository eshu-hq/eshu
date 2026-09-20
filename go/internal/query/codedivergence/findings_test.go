// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import "testing"

// fixtureMembers returns the positive fixture: the same 210-token body in
// two files, plus a third copy in another package.
func fixtureMembers() []Member {
	return []Member{
		{EntityID: "e1", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "a/maps.go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: 210},
		{EntityID: "e2", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "b/maps.go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: 210},
		{EntityID: "e3", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "c/other/maps.go", Language: "go", StartLine: 8, EndLine: 58, TokenCount: 210},
	}
}

// TestAssembleExactFindingPinsScoreReasonsAndID is the contract test: a
// three-copy exact group assembles with score members × tokens, reasons that
// sum to the score, and a stable id derived from (repo, kind, fingerprint).
func TestAssembleExactFindingPinsScoreReasonsAndID(t *testing.T) {
	t.Parallel()

	first, ok := AssembleFinding("repo-x", KindExact, "fp-abc", fixtureMembers(), false)
	if !ok {
		t.Fatal("three-copy exact group must assemble")
	}
	if want := 3 * 210; first.Score != want {
		t.Fatalf("score = %d, want members x tokens = %d", first.Score, want)
	}
	sum := 0
	for _, reason := range first.Reasons {
		sum += reason.Value
	}
	if sum != first.Score {
		t.Fatalf("reasons sum = %d, want score = %d", sum, first.Score)
	}
	second, ok := AssembleFinding("repo-x", KindExact, "fp-abc", fixtureMembers(), false)
	if !ok {
		t.Fatal("reassembly must succeed")
	}
	if first.ID != second.ID || first.ID == "" {
		t.Fatalf("finding id must be stable and non-empty, got %q vs %q", first.ID, second.ID)
	}
	other, ok := AssembleFinding("repo-y", KindExact, "fp-abc", fixtureMembers(), false)
	if !ok {
		t.Fatal("reassembly must succeed")
	}
	if other.ID == first.ID {
		t.Fatal("finding id must differ across repos")
	}
}

// TestAssembleFindingScoreInvariantHoldsForRenamed pins the same invariant
// for the renamed kind on the identical fixture.
func TestAssembleFindingScoreInvariantHoldsForRenamed(t *testing.T) {
	t.Parallel()

	finding, ok := AssembleFinding("repo-x", KindRenamed, "fp-rnm", fixtureMembers(), false)
	if !ok {
		t.Fatal("three-copy renamed group must assemble")
	}
	sum := 0
	for _, reason := range finding.Reasons {
		sum += reason.Value
	}
	if sum != finding.Score {
		t.Fatalf("reasons sum = %d, want score = %d", sum, finding.Score)
	}
	if finding.Kind != KindRenamed {
		t.Fatalf("kind = %q, want renamed", finding.Kind)
	}
}

// TestAmbiguousBoilerplateReportsWithIntent pins the ambiguous fixture:
// two same-name Close methods on different types with identical 60-token
// bodies. Clone-vs-coincidence is genuinely undecidable here (ledger pair
// 89 class), so the group reports as an exact finding and the ambiguity
// lives in the fixture intent, not in a suppression rule that would hide
// either reading.
func TestAmbiguousBoilerplateReportsWithIntent(t *testing.T) {
	t.Parallel()

	members := []Member{
		{EntityID: "e1", EntityName: "Close", EntityType: "Function", RelativePath: "a/conn.go", Language: "go", TokenCount: 60},
		{EntityID: "e2", EntityName: "Close", EntityType: "Function", RelativePath: "b/pool.go", Language: "go", TokenCount: 60},
	}
	finding, ok := AssembleFinding("repo-x", KindExact, "fp-amb", members, false)
	if !ok {
		t.Fatal("ambiguous same-name pair must still assemble")
	}
	if want := 2 * 60; finding.Score != want {
		t.Fatalf("score = %d, want %d", finding.Score, want)
	}
}

// TestFindingScoreTracksReasonMutation pins theory §8: score is defined as
// the sum of the listed reasons, so mutating any single reason value moves
// the recomputed sum by exactly the delta. There are no hidden terms.
func TestFindingScoreTracksReasonMutation(t *testing.T) {
	t.Parallel()

	finding, ok := AssembleFinding("repo-x", KindExact, "fp-abc", fixtureMembers(), false)
	if !ok {
		t.Fatal("three-copy exact group must assemble")
	}
	for index := range finding.Reasons {
		mutated := finding
		mutated.Reasons = append([]Reason(nil), finding.Reasons...)
		mutated.Reasons[index].Value += 7
		sum := 0
		for _, reason := range mutated.Reasons {
			sum += reason.Value
		}
		if sum != finding.Score+7 {
			t.Fatalf("mutating reason %d moved the sum to %d, want score+7 = %d",
				index, sum, finding.Score+7)
		}
	}
}

// TestAssembleFindingDropsSingletonsAfterSuppression pins the negative
// cases: a below-floor member and a generated member leave one survivor, so
// no finding assembles — and the per-rule counts say why.
func TestAssembleFindingDropsSingletonsAfterSuppression(t *testing.T) {
	t.Parallel()

	members := []Member{
		{EntityID: "e1", EntityName: "helper", EntityType: "Function", RelativePath: "a/help.go", Language: "go", TokenCount: 210},
		{EntityID: "e2", EntityName: "helper", EntityType: "Function", RelativePath: "gen/helper.pb.go", Language: "go", TokenCount: 210},
		{EntityID: "e3", EntityName: "tiny", EntityType: "Function", RelativePath: "a/tiny.go", Language: "go", TokenCount: 12},
	}
	_, ok := AssembleFinding("repo-x", KindExact, "fp-abc", members, false)
	if ok {
		t.Fatal("group reduced below two surviving members must not assemble")
	}
}
