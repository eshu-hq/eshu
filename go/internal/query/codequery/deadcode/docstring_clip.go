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
