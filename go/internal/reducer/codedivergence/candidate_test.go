// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"testing"

	querycodedivergence "github.com/eshu-hq/eshu/go/internal/query/codedivergence"
)

// shingleSet builds a deterministic shingle set of size n starting at base.
func shingleSet(base uint64, n int) []uint64 {
	out := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, base+uint64(i))
	}
	return out
}

func driftedMember(id, name, path string, shingles []uint64) MemberRow {
	return MemberRow{
		EntityID:     id,
		EntityName:   name,
		EntityType:   "Function",
		RelativePath: path,
		Language:     "go",
		StartLine:    10,
		EndLine:      40,
		TokenCount:   64,
		Shingles:     shingles,
		FPExact:      "exact-" + id,
		FPRenamed:    "renamed-" + id,
	}
}

// TestApplyRulesAdmitsDriftedPair proves the positive leg: a pair verifying
// at/above threshold with clean members admits with similarity and
// differs_in_ranges evidence atoms.
func TestApplyRulesAdmitsDriftedPair(t *testing.T) {
	t.Parallel()

	// inter 8, union 10: Jaccard 0.8.
	a := driftedMember("e1", "big", "a.go", shingleSet(1, 9))
	b := driftedMember("e2", "bigCopy", "b.go", append(shingleSet(1, 8), 101))
	admitted, suppressed, reason := ApplyRules(CandidatePair{A: a, B: b, SharedBands: 9})
	if suppressed {
		t.Fatalf("drifted pair suppressed under %q, want admitted", reason)
	}
	if admitted.Similarity != 0.8 {
		t.Fatalf("similarity = %v, want 0.8", admitted.Similarity)
	}
	var hasSimilarity, hasRanges bool
	for _, atom := range admitted.Evidence {
		switch atom.EvidenceType {
		case EvidenceTypeSimilarity:
			hasSimilarity = true
		case EvidenceTypeDiffersInRanges:
			hasRanges = true
		}
	}
	if !hasSimilarity || !hasRanges {
		t.Fatalf("admitted pair lacks drift evidence atoms: %+v", admitted.Evidence)
	}
}

// TestApplyRulesRejectsBelowThreshold proves the negative leg: a pair under
// the ship threshold rejects under similarity_below_threshold and never
// admits.
func TestApplyRulesRejectsBelowThreshold(t *testing.T) {
	t.Parallel()

	// inter 6, union 14: Jaccard ~0.43, below the ship threshold.
	a := driftedMember("e1", "big", "a.go", shingleSet(1, 10))
	b := driftedMember("e2", "other", "b.go", append(shingleSet(1, 6), 101, 102, 103, 104))
	_, suppressed, reason := ApplyRules(CandidatePair{A: a, B: b, SharedBands: 3})
	if !suppressed {
		t.Fatal("below-threshold pair admitted, want suppressed")
	}
	if reason != ReasonSimilarityBelowThreshold {
		t.Fatalf("reason = %q, want %q", reason, ReasonSimilarityBelowThreshold)
	}
}

// TestApplyRulesSuppressesGeneratedMember proves the suppression leg: a
// generated-file member suppresses the pair under the reused #6836 rule
// name, counted never silent.
func TestApplyRulesSuppressesGeneratedMember(t *testing.T) {
	t.Parallel()

	a := driftedMember("e1", "big", "a.go", shingleSet(1, 9))
	b := driftedMember("e2", "bigCopy", "gen/b.pb.go", append(shingleSet(1, 8), 101))
	b.FPExact = "exact-e2"
	b.FPRenamed = "renamed-e2"
	_, suppressed, reason := ApplyRules(CandidatePair{A: a, B: b, SharedBands: 9})
	if !suppressed {
		t.Fatal("generated-member pair admitted, want suppressed")
	}
	if reason != querycodedivergence.RuleGenerated {
		t.Fatalf("reason = %q, want %q", reason, querycodedivergence.RuleGenerated)
	}
}

// TestApplyRulesDropsBelowFloorMember proves the ambiguous leg: a member
// under the token floor drops the pair under below_floor even when the
// shingle sets would verify.
func TestApplyRulesDropsBelowFloorMember(t *testing.T) {
	t.Parallel()

	a := driftedMember("e1", "big", "a.go", shingleSet(1, 9))
	b := driftedMember("e2", "tiny", "b.go", append(shingleSet(1, 8), 101))
	b.TokenCount = 12
	_, suppressed, reason := ApplyRules(CandidatePair{A: a, B: b, SharedBands: 9})
	if !suppressed {
		t.Fatal("below-floor pair admitted, want suppressed")
	}
	if reason != querycodedivergence.RuleBelowFloor {
		t.Fatalf("reason = %q, want %q", reason, querycodedivergence.RuleBelowFloor)
	}
}

// TestDriftedFindingIDIsOrientationStable proves the pair identity is
// order-independent: the same two entities in either orientation share one
// finding id, so retries and loader row order never duplicate a row.
func TestDriftedFindingIDIsOrientationStable(t *testing.T) {
	t.Parallel()

	a := driftedMember("e1", "big", "a.go", shingleSet(1, 9))
	b := driftedMember("e2", "bigCopy", "b.go", append(shingleSet(1, 8), 101))
	if got := DriftedFindingID("repo-1", a, b); got != DriftedFindingID("repo-1", b, a) {
		t.Fatal("finding id depends on pair orientation")
	}
	if got := DriftedFindingID("repo-1", a, b); got == "" {
		t.Fatal("finding id is empty")
	}
	if got, other := DriftedFindingID("repo-1", a, b), DriftedFindingID("repo-2", a, b); got == other {
		t.Fatal("finding id ignores repo")
	}
}
