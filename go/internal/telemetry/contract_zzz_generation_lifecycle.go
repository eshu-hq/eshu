// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// SpanQueryFreshnessGenerationLifecycle wraps the bounded generation lifecycle
// drilldown read from scope_generations and fact_work_items.
const SpanQueryFreshnessGenerationLifecycle = "query.freshness_generation_lifecycle"

const (
	// SpanAttrGenerationLifecycleResultCount records returned generation
	// lifecycle rows after page truncation.
	SpanAttrGenerationLifecycleResultCount = "eshu.generation_lifecycle.result_count"
	// SpanAttrGenerationLifecycleTruncated records whether limit+1 pagination
	// found another generation lifecycle page.
	SpanAttrGenerationLifecycleTruncated = "eshu.generation_lifecycle.truncated"
	// SpanAttrGenerationLifecycleActiveCount records returned rows whose
	// generation is the scope's current active generation.
	SpanAttrGenerationLifecycleActiveCount = "eshu.generation_lifecycle.active_count"
	// SpanAttrGenerationLifecycleFailureCount records returned rows that carry a
	// latest failure class.
	SpanAttrGenerationLifecycleFailureCount = "eshu.generation_lifecycle.failure_count"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryWorkItemEvidence {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryFreshnessGenerationLifecycle)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryFreshnessGenerationLifecycle)
}
