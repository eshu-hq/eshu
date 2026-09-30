// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// clipDeadCodeInvestigationDocstrings applies the read-time docstring clip
// (#7234) to every row in the three investigation buckets and returns how many
// rows it clipped. The buckets are the whole set of rows the reply carries, and
// each row echoes its docstring into metadata and the derived semantic fields,
// so an unclipped page overruns the MCP response budget. Call it after the scan
// has trimmed and bounded the buckets and before the analysis reads the rows, so
// the count matches the rows returned and the analysis sees clipped values.
func clipDeadCodeInvestigationDocstrings(scan *DeadCodeInvestigationScan) int {
	clipped := 0
	for _, bucket := range [][]map[string]any{scan.CleanupReady, scan.Ambiguous, scan.Suppressed} {
		clipped += querycontract.ClipRowsDocstring(bucket, entitysemantics.ReattachSemanticSummary)
	}
	return clipped
}

// clipCrossRepoDeadCodeDocstrings applies the same clip to the active and
// suppressed rows of a cross-repo scan, which the reply buckets into dead,
// live_by_consumer, unknown, and suppressed. It returns how many rows it
// clipped. Call it after the scan has bounded both slices and before the rows
// are cloned into buckets.
func clipCrossRepoDeadCodeDocstrings(scan *CrossRepoDeadCodeScan) int {
	clipped := querycontract.ClipRowsDocstring(scan.Active, entitysemantics.ReattachSemanticSummary)
	return clipped + querycontract.ClipRowsDocstring(scan.Suppressed, entitysemantics.ReattachSemanticSummary)
}

// withDocstringClipMarkers adds the response-level docstring clip markers to
// data and returns it, so a handler can wrap a response literal in place.
func withDocstringClipMarkers(clipped int, data map[string]any) map[string]any {
	querycontract.AddDocstringClipMarkers(data, clipped)
	return data
}
