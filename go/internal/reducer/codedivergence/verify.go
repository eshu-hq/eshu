// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

// DriftedSimilarityThreshold is the ship threshold for drifted findings
// from the #6834 theory proof (§5): Jaccard ≥ 0.7 over renamed 5-shingles
// measured ≥98% precision on 500 hand-labelled pairs. Pairs in 0.5–0.7 stay
// available ranked, never finding-grade. Do not change without re-running
// the precision study and updating the evidence doc.
const DriftedSimilarityThreshold = 0.7

// MaxCandidatesPerEntity is the per-entity verification budget from the
// #6834 theory proof (§6): each entity verifies at most K partners, ordered
// by shared-band count desc. K = 200 preserves 100% of measured ship-band
// recall with work bounded per entity regardless of mega-row size (this
// replaces the disproven row-cap-50). Entities that exhaust the budget are
// counted in telemetry, never silently dropped.
const MaxCandidatesPerEntity = 200

// MaxBandBucketSize caps the band buckets the nomination joins: a
// (band_no, band_hash) bucket with more members than the per-entity
// budget nominates no pairs (#7228). A bucket that big hands every
// member more same-bucket partners than the budget verifies, so it is
// non-discriminating by the query's own semantics (an LSH stop band),
// and joining it is quadratic in the bucket size. Buckets at or under
// the cap nominate exactly as before. Skipped buckets are counted in
// telemetry, never silently dropped.
const MaxBandBucketSize = MaxCandidatesPerEntity

// Jaccard returns the exact Jaccard similarity |A∩B|/|A∪B| over two shingle
// identity sets. It is order-independent (map intersection, not merge-join)
// so rows persisted before the sorted encoding stay verifiable. Two empty
// sets carry no similarity evidence and score 0.0: an empty set means "not
// fingerprinted", never "identical".
func Jaccard(a, b []uint64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	var smaller, larger []uint64
	if len(a) < len(b) {
		smaller, larger = a, b
	} else {
		smaller, larger = b, a
	}
	inLarger := make(map[uint64]bool, len(larger))
	for _, id := range larger {
		inLarger[id] = true
	}
	inter := 0
	seen := make(map[uint64]bool, len(smaller))
	for _, id := range smaller {
		if seen[id] {
			continue
		}
		seen[id] = true
		if inLarger[id] {
			inter++
		}
	}
	union := len(inLarger) + len(seen) - inter
	if union == 0 {
		return 0.0
	}
	return float64(inter) / float64(union)
}

// Admitted reports whether a candidate pair's shingle sets verify at or
// above the ship threshold: the pair becomes a drifted finding. Identical
// pairs (1.0) admit here but are normally claimed by the equality read
// surface first; the reducer dedupes them before writing.
func Admitted(a, b []uint64) bool {
	return Jaccard(a, b) >= DriftedSimilarityThreshold
}
