// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"math"
	"sort"
)

// sortEvidenceFactsForAggregation returns the facts ordered by a content key:
// clamped confidence descending, then evidence kind, path, matched value,
// raw confidence, Details serialization, rationale, and repo IDs, all
// ascending except the confidences. The trailing comparisons exist so the
// order is total over everything aggregateCandidate accumulates: two facts
// that tie on every preview-relevant field can still differ in raw
// confidence (the preview records the unclamped value), unrelated Details
// keys, rationale (join order), or repo IDs (first-non-empty wins), and
// without a tie-break their relative order — and therefore the candidate —
// would still depend on arrival order. fmt prints maps with sorted keys, so
// the Details comparison is deterministic. The input slice is never mutated:
// callers retain their arrival-ordered buckets.
func sortEvidenceFactsForAggregation(facts []EvidenceFact) []EvidenceFact {
	ordered := make([]EvidenceFact, len(facts))
	copy(ordered, facts)
	sort.Slice(ordered, func(i, j int) bool {
		ci, cj := clampConfidence(ordered[i].Confidence), clampConfidence(ordered[j].Confidence)
		if ci != cj {
			return ci > cj
		}
		if ordered[i].EvidenceKind != ordered[j].EvidenceKind {
			return ordered[i].EvidenceKind < ordered[j].EvidenceKind
		}
		pi, pj := toDetailsString(ordered[i].Details["path"]), toDetailsString(ordered[j].Details["path"])
		if pi != pj {
			return pi < pj
		}
		mi, mj := toDetailsString(ordered[i].Details["matched_value"]), toDetailsString(ordered[j].Details["matched_value"])
		if mi != mj {
			return mi < mj
		}
		if ri, rj := ordered[i].Confidence, ordered[j].Confidence; ri != rj {
			// NaN has no magnitude: != is always true for it but NaN > x is
			// always false, so without this branch a NaN fact
			// short-circuits to arrival order without consulting the deeper
			// content keys. Rank a lone NaN below every real value --
			// unknown confidence must not outrank a measured one -- while a
			// NaN/NaN pair counts as tied here so Details, rationale, and
			// repo IDs below still decide, keeping the key total.
			// (Codex #6184 P2.)
			if math.IsNaN(ri) != math.IsNaN(rj) {
				return math.IsNaN(rj)
			}
			if !math.IsNaN(ri) {
				return ri > rj
			}
		}
		// %#v, not %v: %v collides across types (1 vs "1"), which would
		// leave a type-divergent tie to arrival order. Both verbs print
		// maps with sorted keys, so determinism is preserved. (#6184 P3)
		if di, dj := fmt.Sprintf("%#v", ordered[i].Details), fmt.Sprintf("%#v", ordered[j].Details); di != dj {
			return di < dj
		}
		// Rationale and repo IDs are struct fields, not Details entries, but
		// they feed order-sensitive accumulations too (rationale join order,
		// first-non-empty repo), so they close the key. (#6184 review F1)
		if ordered[i].Rationale != ordered[j].Rationale {
			return ordered[i].Rationale < ordered[j].Rationale
		}
		if ordered[i].SourceRepoID != ordered[j].SourceRepoID {
			return ordered[i].SourceRepoID < ordered[j].SourceRepoID
		}
		return ordered[i].TargetRepoID < ordered[j].TargetRepoID
	})
	return ordered
}
