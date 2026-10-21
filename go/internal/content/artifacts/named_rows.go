// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

// NamedRowsToMaps renders named rows as the row maps the merged load uses:
// one map per row with exactly the four merge-identity keys of
// configPathKeyOrder (path, source_repo, relative_path, evidence_kind). The
// stored path converts its named rows through it and merges with
// MergeConfigPaths, so stored rows dedup against the anchor live rows exactly
// as related live rows do. It returns nil for no rows, keeping the merge's
// nil contract.
func NamedRowsToMaps(rows []NamedRow) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"path":          row.Path,
			"source_repo":   row.SourceRepo,
			"relative_path": row.RelativePath,
			"evidence_kind": row.EvidenceKind,
		})
	}
	return out
}
