// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"math"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestRRFMonotonicity(t *testing.T) {
	t.Parallel()

	// When a document appears in more lists or at a higher rank, its fused
	// score must never decrease — monotonicity is the core RRF property.
	k := 60

	// Same document in more lists → strictly higher score.
	oneList := rrfFuse([][]int{{5}}, k)
	twoLists := rrfFuse([][]int{{5}, {5}}, k)
	if twoLists[5] <= oneList[5] {
		t.Errorf("two-list score %v <= one-list score %v — same doc more lists must increase score",
			twoLists[5], oneList[5])
	}

	// Same document at a higher (better) rank → strictly higher score.
	ranked := rrfFuse([][]int{{3, 7}}, k)
	if ranked[3] <= ranked[7] {
		t.Errorf("rank-0 score %v <= rank-1 score %v — better rank must increase score",
			ranked[3], ranked[7])
	}

	// Same-rank-in-both-lists must score higher than in-one-list-only.
	bothTop := rrfFuse([][]int{{0, 99}, {0, 42}}, k)
	oneTop := rrfFuse([][]int{{0, 99}}, k)
	if bothTop[0] <= oneTop[0] {
		t.Errorf("both-top score %v <= one-top score %v — violates monotonicity",
			bothTop[0], oneTop[0])
	}
}

func TestRRFTieBreak(t *testing.T) {
	t.Parallel()

	// When two documents have identical fused scores, the tie is broken by
	// document ID in ascending order — guaranteeing deterministic output
	// for fixed inputs.

	// Documents with IDs that sort a < b.
	docs := []searchdocs.Document{
		{
			ID: "a", Title: "alpha", ContextText: "x", RepoID: "r",
			SourceKind: searchdocs.SourceKindCodeEntity,
			TruthScope: searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
			Freshness:  searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		},
		{
			ID: "b", Title: "beta", ContextText: "x", RepoID: "r",
			SourceKind: searchdocs.SourceKindCodeEntity,
			TruthScope: searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
			Freshness:  searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		},
	}
	index := mustIndex(t, docs, Options{})

	// Same score for both = 1.0.
	scores := map[int]float64{0: 1.0, 1: 1.0}
	ordered := index.rankByScore(scores)

	if len(ordered) != 2 {
		t.Fatalf("expected 2 ranked docs, got %d", len(ordered))
	}
	if index.documents[ordered[0]].doc.ID != "a" {
		t.Errorf("tie-break: first = %q, want a (ascending id)", index.documents[ordered[0]].doc.ID)
	}
	if index.documents[ordered[1]].doc.ID != "b" {
		t.Errorf("tie-break: second = %q, want b", index.documents[ordered[1]].doc.ID)
	}
}

func TestRRFDegradedToBM25(t *testing.T) {
	t.Parallel()

	// When useVector is false, hybrid mode must degenerate to BM25-only —
	// returning the BM25 ranking as-is with method "bm25". This is the path
	// taken when no embedder is configured.

	index := mustIndex(t, corpus(), Options{})
	backend := Backend{Index: index}

	queryTerms := tokenCounts("payment refund")
	inScope := func(i int) bool { return index.documents[i].doc.RepoID == "repo-1" }
	bm25Scores := index.bm25ScoredInScope(queryTerms, inScope)
	bm25Ranked := positiveScored(index.rankByScore(bm25Scores), bm25Scores)

	req := request("payment refund", "repo-1", searchbench.ModeHybrid, 5)
	ordered, _, method := backend.rankForMode(req, index, modeInputs{
		bm25Ranked:   bm25Ranked,
		bm25Scores:   bm25Scores,
		vectorRanked: nil,
		vectorScores: nil,
		useVector:    false,
	})

	if method != "bm25" {
		t.Errorf("degraded method = %q, want bm25", method)
	}
	if len(ordered) == 0 {
		t.Fatal("degraded hybrid returned empty ranking")
	}

	// Ordering must match BM25-only ordering.
	bm25Req := request("payment refund", "repo-1", searchbench.ModeKeyword, 5)
	bm25Only, _, bm25Method := backend.rankForMode(bm25Req, index, modeInputs{
		bm25Ranked:   bm25Ranked,
		bm25Scores:   bm25Scores,
		vectorRanked: nil,
		vectorScores: nil,
		useVector:    false,
	})
	if bm25Method != "bm25" {
		t.Errorf("keyword method = %q, want bm25", bm25Method)
	}
	if len(ordered) != len(bm25Only) {
		t.Fatalf("degraded len = %d, bm25 len = %d", len(ordered), len(bm25Only))
	}
	for i := range ordered {
		if ordered[i] != bm25Only[i] {
			t.Errorf("rank %d: degraded = %d, bm25 = %d", i, ordered[i], bm25Only[i])
		}
	}
}

func TestRRFConstantSensitivity(t *testing.T) {
	t.Parallel()

	// Changing k changes RRF sensitivity to rank differences. A smaller k
	// makes top ranks much more valuable relative to lower ones, while a
	// larger k compresses scores toward uniformity.

	// One ranked list with the top-10 documents.
	lists := [][]int{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}}

	// Small k — large spread between rank 1 and rank 10.
	smallK := rrfFuse(lists, 5)
	// Large k — compressed spread between rank 1 and rank 10.
	largeK := rrfFuse(lists, 120)

	if smallK[0] <= 0 || largeK[0] <= 0 {
		t.Fatal("top-rank score must be positive")
	}

	// The ratio (rank 1 / rank 10) must be larger for smaller k: rank
	// position matters more when k is small.
	smallRatio := smallK[0] / smallK[9]
	largeRatio := largeK[0] / largeK[9]

	if smallRatio <= largeRatio {
		t.Errorf("small-k ratio %v <= large-k ratio %v — small k should amplify rank differences",
			smallRatio, largeRatio)
	}

	// As k → ∞, all scores converge to 1/k. Verify large k compresses
	// scores toward uniformity: max-min spread smaller for large k.
	smallSpread := maxScore(smallK) - minNonZero(smallK)
	largeSpread := maxScore(largeK) - minNonZero(largeK)
	if smallSpread <= largeSpread {
		t.Errorf("small-k spread %v <= large-k spread %v — large k should compress scores",
			smallSpread, largeSpread)
	}
}

func TestRankPositionsOneBased(t *testing.T) {
	t.Parallel()

	ordered := []int{42, 7, 99}
	positions := rankPositions(ordered)

	if p := positions[42]; p != 1 {
		t.Errorf("position[42] = %d, want 1", p)
	}
	if p := positions[7]; p != 2 {
		t.Errorf("position[7] = %d, want 2", p)
	}
	if p := positions[99]; p != 3 {
		t.Errorf("position[99] = %d, want 3", p)
	}
	if _, ok := positions[0]; ok {
		t.Error("unlisted element must be absent")
	}
}

func TestTruncateBounds(t *testing.T) {
	t.Parallel()

	list := []int{10, 20, 30, 40, 50}

	if got := truncate(list, 3); len(got) != 3 || got[2] != 30 {
		t.Fatalf("truncate(list, 3) = %v", got)
	}
	if got := truncate(list, 10); len(got) != 5 {
		t.Fatalf("truncate(list, 10) = %v (should return full list)", got)
	}
	if got := truncate(list, 0); len(got) != 0 {
		t.Fatalf("truncate(list, 0) = %v (should be empty)", got)
	}
	if got := truncate(list, -1); len(got) != 0 {
		t.Fatalf("truncate(list, -1) = %v (negative treated as zero)", got)
	}
	if got := truncate([]int{}, 5); len(got) != 0 {
		t.Fatalf("truncate([], 5) = %v (should be empty)", got)
	}
}

func TestCandidatePoolSizeBounds(t *testing.T) {
	t.Parallel()

	if got := candidatePoolSize(1); got != candidatePoolFloor {
		t.Errorf("candidatePoolSize(1) = %d, want floor %d", got, candidatePoolFloor)
	}
	if got := candidatePoolSize(3); got != candidatePoolFloor {
		t.Errorf("candidatePoolSize(3) = %d, want floor %d", got, candidatePoolFloor)
	}
	if got := candidatePoolSize(100); got != candidatePoolCeil {
		t.Errorf("candidatePoolSize(100) = %d, want ceiling %d", got, candidatePoolCeil)
	}
	if got := candidatePoolSize(15); got != 150 {
		t.Errorf("candidatePoolSize(15) = %d, want 150", got)
	}
}

func TestPositiveScoredFilters(t *testing.T) {
	t.Parallel()

	ordered := []int{0, 1, 2, 3}
	scores := map[int]float64{0: 1.0, 1: 0.0, 2: 0.5, 3: 0.0}
	kept := positiveScored(ordered, scores)

	if len(kept) != 2 {
		t.Fatalf("positiveScored = %d, want 2", len(kept))
	}
	if kept[0] != 0 || kept[1] != 2 {
		t.Errorf("positiveScored = %v, want [0, 2]", kept)
	}

	// All zeros returns empty.
	allZero := positiveScored([]int{0, 1}, map[int]float64{0: 0.0, 1: 0.0})
	if len(allZero) != 0 {
		t.Errorf("all-zero positiveScored = %d, want 0", len(allZero))
	}

	// Negative scores are excluded (not strictly positive).
	neg := positiveScored([]int{0}, map[int]float64{0: -0.1})
	if len(neg) != 0 {
		t.Errorf("negative positiveScored = %d, want 0", len(neg))
	}
}

func TestRRFFuseEmptyInputs(t *testing.T) {
	t.Parallel()

	// Empty list of lists returns empty map.
	if got := rrfFuse(nil, 60); len(got) != 0 {
		t.Errorf("rrfFuse(nil) = %d entries, want 0", len(got))
	}
	// Two empty lists.
	if got := rrfFuse([][]int{{}, {}}, 60); len(got) != 0 {
		t.Errorf("rrfFuse(empty,empty) = %d entries, want 0", len(got))
	}
}

func TestRRFFuseZeroK(t *testing.T) {
	t.Parallel()

	// k=0 is valid RRF — score is 1/rank.
	lists := [][]int{{0, 1, 2}}
	fused := rrfFuse(lists, 0)

	// Rank 0 (1st) = 1/1 = 1.0, rank 1 (2nd) = 1/2 = 0.5, etc.
	if math.Abs(fused[0]-1.0) > 1e-9 {
		t.Errorf("rrfFuse(k=0)[0] = %v, want 1.0", fused[0])
	}
	if math.Abs(fused[1]-0.5) > 1e-9 {
		t.Errorf("rrfFuse(k=0)[1] = %v, want 0.5", fused[1])
	}
	if math.Abs(fused[2]-1.0/3.0) > 1e-9 {
		t.Errorf("rrfFuse(k=0)[2] = %v, want ~0.333", fused[2])
	}
}

func maxScore(m map[int]float64) float64 {
	best := 0.0
	for _, v := range m {
		if v > best {
			best = v
		}
	}
	return best
}

func minNonZero(m map[int]float64) float64 {
	best := math.MaxFloat64
	for _, v := range m {
		if v > 0 && v < best {
			best = v
		}
	}
	if best == math.MaxFloat64 {
		return 0
	}
	return best
}
