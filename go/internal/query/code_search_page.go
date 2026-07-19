// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func codeSearchProbeLimit(publicLimit int) int {
	return publicLimit + 1
}

func codeSearchPagePayload(
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
	return map[string]any{
		"source":         source,
		"source_backend": sourceBackend,
		"query":          query,
		"repo_id":        repositoryID,
		"results":        rows,
		"matches":        rows,
		"count":          len(rows),
		"limit":          publicLimit,
		"truncated":      truncated,
	}
}
