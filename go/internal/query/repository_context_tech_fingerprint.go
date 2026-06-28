// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// buildLanguageBreakdownFromRows converts the []map[string]any rows produced by
// queryRepoLanguageDistribution (or repositoryLanguageDistributionFromCoverage)
// into a compact language→file_count map for the tech-fingerprint rollup.
// Rows with an empty language key are silently skipped.
func buildLanguageBreakdownFromRows(rows []map[string]any) map[string]int {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		lang := StringVal(row, "language")
		if lang == "" {
			continue
		}
		out[lang] = IntVal(row, "file_count")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildSourceToolBreakdownFromRows converts the []map[string]any rows produced
// by queryRepoSourceToolBreakdown into a compact source_tool→edge_count map for
// the tech-fingerprint rollup. Rows with an empty source_tool key are skipped.
func buildSourceToolBreakdownFromRows(rows []map[string]any) map[string]int {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		tool := StringVal(row, "source_tool")
		if tool == "" {
			continue
		}
		out[tool] = IntVal(row, "edge_count")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// queryServiceTechFingerprint computes language_breakdown and
// source_tool_breakdown for a service, anchored on the service's primary
// repository ID. When the service has no resolved repo_id the result is empty.
// Both breakdowns are read-only aggregates — no graph writes or mutations.
func queryServiceTechFingerprint(
	ctx context.Context,
	reader GraphQuery,
	workloadContext map[string]any,
) (languageBreakdown map[string]int, sourceToolBreakdown map[string]int) {
	repoID := safeStr(workloadContext, "repo_id")
	if repoID == "" || reader == nil {
		return nil, nil
	}
	params := map[string]any{"repo_id": repoID}

	languageBreakdown = buildLanguageBreakdownFromRows(queryRepoLanguageDistribution(ctx, reader, params))
	sourceToolBreakdown = buildSourceToolBreakdownFromRows(queryRepoSourceToolBreakdown(ctx, reader, params))
	return languageBreakdown, sourceToolBreakdown
}
