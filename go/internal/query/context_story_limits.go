// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// contextStoryItemLimit bounds relationship and instance fan-out attached to
// entity and workload context/story payloads so a single prompt-ready read
// stays within the route budget and exposes truncation explicitly. The value
// moved to querycontract for #6060 so the impact handler-family subpackages
// can share it without importing this package; this alias keeps root callers
// unchanged.
const contextStoryItemLimit = querycontract.ContextStoryItemLimit

// workloadContextResultLimits builds the shared result_limits drilldown block
// for a workload context or story payload. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func workloadContextResultLimits(ctx map[string]any, workloadID, surface string) map[string]any {
	return querycontract.WorkloadContextResultLimits(ctx, workloadID, surface)
}

// contextPartialReasons promotes the context payload's limitations into an
// explicit partial_reasons array. The implementation moved to querycontract
// for #6060; this wrapper keeps root callers unchanged.
func contextPartialReasons(ctx map[string]any, extra ...string) []string {
	return querycontract.ContextPartialReasons(ctx, extra...)
}
