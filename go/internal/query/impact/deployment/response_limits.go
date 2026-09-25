// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// hostnameEntrypointDrilldownTool names the read a caller uses after
// hostnames or entrypoints were cut from the trace response. It is the
// workload context route, which caps the same lists at the same limit and
// reports their totals on result_limits.
const hostnameEntrypointDrilldownTool = "get_workload_context"

// attachHostnameEntrypointRows emits hostnames and entrypoints on the trace
// response, each cut to querycontract.ContextStoryItemLimit, and adds
// hostname_limits and entrypoint_limits (limit, total, truncated) whenever the
// list is non-empty. The cut happens at emission, after artifact lineage,
// story, and overview counts have read the full lists, so it changes only what
// ships (#7169). truncated is true exactly when rows were dropped, so a cut is
// never silent.
func (f *deploymentTraceFields) attachHostnameEntrypointRows(response map[string]any) {
	attachCappedRows(response, "hostnames", "hostname_limits", f.hostnames)
	attachCappedRows(response, "entrypoints", "entrypoint_limits", f.entrypoints)
}

func attachCappedRows(response map[string]any, key, limitsKey string, rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	capped, truncated := querycontract.CapMapRows(rows, querycontract.ContextStoryItemLimit)
	response[key] = capped
	response[limitsKey] = map[string]any{
		"limit":          querycontract.ContextStoryItemLimit,
		"total":          len(rows),
		"truncated":      truncated,
		"drilldown_tool": hostnameEntrypointDrilldownTool,
	}
}
