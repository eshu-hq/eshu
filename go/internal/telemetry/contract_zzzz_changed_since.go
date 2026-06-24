// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// SpanQueryFreshnessChangedSince wraps the bounded changed-since delta read that
// diffs a prior generation's fact set against the current active generation's
// fact set in fact_records.
const SpanQueryFreshnessChangedSince = "query.freshness_changed_since"

const (
	// SpanAttrChangedSinceScopeID records the resolved ingestion scope id of the
	// changed-since diff.
	SpanAttrChangedSinceScopeID = "eshu.changed_since.scope_id"
	// SpanAttrChangedSinceSinceGenerationID records the prior generation the diff
	// compared against.
	SpanAttrChangedSinceSinceGenerationID = "eshu.changed_since.since_generation_id"
	// SpanAttrChangedSinceCurrentGenerationID records the current active
	// generation the diff compared against.
	SpanAttrChangedSinceCurrentGenerationID = "eshu.changed_since.current_generation_id"
	// SpanAttrChangedSinceChangedCount records the total added, updated, retired,
	// and superseded keys across all categories (the non-unchanged delta size).
	SpanAttrChangedSinceChangedCount = "eshu.changed_since.changed_count"
	// SpanAttrChangedSinceUnavailable records whether the diff could not be
	// computed because there was no current active generation.
	SpanAttrChangedSinceUnavailable = "eshu.changed_since.unavailable"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryFreshnessGenerationLifecycle {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryFreshnessChangedSince)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryFreshnessChangedSince)
}
