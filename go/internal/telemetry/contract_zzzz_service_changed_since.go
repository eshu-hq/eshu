// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// SpanQueryFreshnessServiceChangedSince wraps the bounded service-scope
// changed-since delta read (#1943) that diffs a prior service materialization
// generation's evidence snapshot set against the current active generation's set
// in service_evidence_snapshots.
const SpanQueryFreshnessServiceChangedSince = "query.freshness_service_changed_since"

const (
	// SpanAttrServiceChangedSinceServiceID records the resolved service id of the
	// service-scope changed-since diff.
	SpanAttrServiceChangedSinceServiceID = "eshu.service_changed_since.service_id"
	// SpanAttrServiceChangedSinceSinceGenerationID records the prior service
	// generation the diff compared against.
	SpanAttrServiceChangedSinceSinceGenerationID = "eshu.service_changed_since.since_generation_id"
	// SpanAttrServiceChangedSinceCurrentGenerationID records the current active
	// service generation the diff compared against.
	SpanAttrServiceChangedSinceCurrentGenerationID = "eshu.service_changed_since.current_generation_id"
	// SpanAttrServiceChangedSinceChangedCount records the total added, updated,
	// retired, and superseded evidence keys across all families.
	SpanAttrServiceChangedSinceChangedCount = "eshu.service_changed_since.changed_count"
	// SpanAttrServiceChangedSinceUnavailable records whether the diff could not be
	// computed because the service had no current active generation.
	SpanAttrServiceChangedSinceUnavailable = "eshu.service_changed_since.unavailable"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryFreshnessChangedSince {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryFreshnessServiceChangedSince)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryFreshnessServiceChangedSince)
}
