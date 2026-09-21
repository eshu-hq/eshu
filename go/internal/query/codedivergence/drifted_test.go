// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"strings"
	"testing"
)

// driftedFixtureRow returns one admitted pair as the reducer wrote it: two
// 210-token members at Jaccard 0.87 over the 0.70 threshold, nominated by 9
// shared LSH bands.
func driftedFixtureRow() DriftedRow {
	return DriftedRow{
		FindingID:   "abc123def4567890",
		Similarity:  0.87,
		Threshold:   0.70,
		SharedBands: 9,
		Members: []Member{
			{EntityID: "e1", EntityName: "renderTable", EntityType: "Function", RelativePath: "a/table.go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: 210},
			{EntityID: "e2", EntityName: "renderTable", EntityType: "Function", RelativePath: "b/table.go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: 210},
		},
	}
}

// TestAssembleDriftedFindingAdmitsFixture pins the drifted read contract: a
// reducer-admitted pair assembles into a drifted finding scored on the same
// members x tokens currency as the equality kinds, with reasons summing to
// the score without remainder and the writer finding id carried as the
// fingerprint for cross-generation continuity.
func TestAssembleDriftedFindingAdmitsFixture(t *testing.T) {
	t.Parallel()

	finding, ok := AssembleDriftedFinding("repo-x", driftedFixtureRow(), false)
	if !ok {
		t.Fatalf("AssembleDriftedFinding() ok = false, want true")
	}
	if finding.Kind != KindDrifted {
		t.Errorf("Kind = %q, want %q", finding.Kind, KindDrifted)
	}
	if finding.Fingerprint != "abc123def4567890" {
		t.Errorf("Fingerprint = %q, want the writer finding id", finding.Fingerprint)
	}
	if finding.Score != 420 {
		t.Errorf("Score = %d, want 420 (2 members x 210 tokens)", finding.Score)
	}
	sum := 0
	for _, reason := range finding.Reasons {
		sum += reason.Value
	}
	if sum != finding.Score {
		t.Errorf("reasons sum to %d, want score %d (no hidden terms)", sum, finding.Score)
	}
	sentences := make([]string, 0, len(finding.Reasons))
	for _, reason := range finding.Reasons {
		sentences = append(sentences, reason.Sentence)
	}
	joined := strings.Join(sentences, "\n")
	if !strings.Contains(joined, "0.87") || !strings.Contains(joined, "0.7") {
		t.Errorf("reasons must carry the measured similarity and threshold, got:\n%s", joined)
	}
	if len(finding.Suppressions) != 0 {
		t.Errorf("Suppressions = %v, want empty for a clean admit", finding.Suppressions)
	}
}

// TestAssembleDriftedFindingStability pins the id derivation: the same pair
// keeps its finding id while the pair exists, and it differs from the
// writer id only by the documented (repo, kind, fingerprint) derivation.
func TestAssembleDriftedFindingStability(t *testing.T) {
	t.Parallel()

	first, ok := AssembleDriftedFinding("repo-x", driftedFixtureRow(), false)
	if !ok {
		t.Fatalf("AssembleDriftedFinding() ok = false, want true")
	}
	second, ok := AssembleDriftedFinding("repo-x", driftedFixtureRow(), false)
	if !ok {
		t.Fatalf("AssembleDriftedFinding() ok = false, want true")
	}
	if first.ID != second.ID {
		t.Errorf("ID unstable: %q vs %q", first.ID, second.ID)
	}
	if first.ID != StatID("repo-x", KindDrifted, "abc123def4567890") {
		t.Errorf("ID = %q, want the StatID derivation so stat order and finding order agree on ties", first.ID)
	}
}

// TestAssembleDriftedFindingSuppressesBelowFloor is the negative leg: a
// member under the token floor drops the pair and counts below_floor
// instead of reporting a single copy.
func TestAssembleDriftedFindingSuppressesBelowFloor(t *testing.T) {
	t.Parallel()

	row := driftedFixtureRow()
	row.Members[1].TokenCount = 30
	finding, ok := AssembleDriftedFinding("repo-x", row, false)
	if ok {
		t.Fatalf("AssembleDriftedFinding() ok = true, want false for a single survivor")
	}
	if finding.Suppressions[RuleBelowFloor] != 1 {
		t.Errorf("Suppressions = %v, want below_floor = 1", finding.Suppressions)
	}
}

// TestAssembleDriftedFindingTestFileAmbiguity is the ambiguous leg: a
// test-file member suppresses by default but reports with includeTests,
// matching the equality-kind contract.
func TestAssembleDriftedFindingTestFileAmbiguity(t *testing.T) {
	t.Parallel()

	row := driftedFixtureRow()
	row.Members[1].RelativePath = "b/table_test.go"
	suppressed, ok := AssembleDriftedFinding("repo-x", row, false)
	if ok {
		t.Fatalf("AssembleDriftedFinding() ok = true without includeTests, want false")
	}
	if suppressed.Suppressions[RuleTestFile] != 1 {
		t.Errorf("Suppressions = %v, want test_file = 1", suppressed.Suppressions)
	}
	admitted, ok := AssembleDriftedFinding("repo-x", row, true)
	if !ok {
		t.Fatalf("AssembleDriftedFinding() ok = false with includeTests, want true")
	}
	if len(admitted.Members) != 2 {
		t.Errorf("Members = %d, want 2 with includeTests", len(admitted.Members))
	}
}
