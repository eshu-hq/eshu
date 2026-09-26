// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// CodeSearchProbeLimit bounds one code-search page with a limit+1 truncation probe.
func CodeSearchProbeLimit(publicLimit int) int {
	return publicLimit + 1
}

// CodeSearchPagePayload shapes code-search rows into the paged response envelope.
func CodeSearchPagePayload(
	source string,
	sourceBackend string,
	query string,
	repositoryID string,
	rows []map[string]any,
	publicLimit int,
) map[string]any {
	truncated := len(rows) > publicLimit
	if truncated {
		rows = rows[:publicLimit]
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	// Clip after the page is trimmed and after any hybrid re-rank, which reads
	// the full stored body, so the count covers the rows actually returned.
	clippedRows := querycontract.ClipRowsSourceCache(rows)
	payload := map[string]any{
		"source":         source,
		"source_backend": sourceBackend,
		"query":          query,
		"repo_id":        repositoryID,
		"results":        rows,
		"count":          len(rows),
		"limit":          publicLimit,
		"truncated":      truncated,
	}
	querycontract.AddSourceCacheClipMarkers(payload, clippedRows)
	return payload
}
