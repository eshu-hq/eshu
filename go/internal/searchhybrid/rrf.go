// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import "sort"

const (
	// candidatePoolMultiple bounds how many per-ranker candidates feed fusion,
	// relative to the requested limit, so fusion work stays bounded.
	candidatePoolMultiple = 10
	// candidatePoolFloor and candidatePoolCeil bound the per-ranker pool size.
	candidatePoolFloor = 50
	candidatePoolCeil  = 500
)

// rankByScore orders document indices by descending score, breaking ties by
// document id so the ranking is deterministic for fixed inputs.
func (index *Index) rankByScore(scores map[int]float64) []int {
	ordered := make([]int, 0, len(scores))
	for idx := range scores {
		ordered = append(ordered, idx)
	}
	sort.Slice(ordered, func(i int, j int) bool {
		left, right := ordered[i], ordered[j]
		if scores[left] != scores[right] {
			return scores[left] > scores[right]
		}
		return index.documents[left].doc.ID < index.documents[right].doc.ID
	})
	return ordered
}

// rankPositions maps each document index to its 1-based position in the list.
func rankPositions(ordered []int) map[int]int {
	positions := make(map[int]int, len(ordered))
	for position, idx := range ordered {
		positions[idx] = position + 1
	}
	return positions
}

// rrfFuse combines ranked lists into a fused score per document index using
// Reciprocal Rank Fusion: each list contributes 1/(k + rank).
func rrfFuse(lists [][]int, k int) map[int]float64 {
	fused := make(map[int]float64)
	for _, list := range lists {
		for rank, idx := range list {
			fused[idx] += 1.0 / float64(k+rank+1)
		}
	}
	return fused
}

// truncate returns the first n indices, or all of them when n exceeds the list.
func truncate(ordered []int, n int) []int {
	if n < 0 {
		n = 0
	}
	if len(ordered) <= n {
		return ordered
	}
	return ordered[:n]
}

// candidatePoolSize bounds the per-ranker candidate pool feeding fusion.
func candidatePoolSize(limit int) int {
	size := limit * candidatePoolMultiple
	if size < candidatePoolFloor {
		size = candidatePoolFloor
	}
	if size > candidatePoolCeil {
		size = candidatePoolCeil
	}
	return size
}

// positiveScored returns the indices whose score is strictly positive, in the
// given ranked order. BM25 yields a positive score only when a query term
// matched, so this drops documents with no lexical overlap.
func positiveScored(ordered []int, scores map[int]float64) []int {
	kept := make([]int, 0, len(ordered))
	for _, idx := range ordered {
		if scores[idx] > 0 {
			kept = append(kept, idx)
		}
	}
	return kept
}
