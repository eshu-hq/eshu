// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "fmt"

// scopedCodeTopicFileBranch preserves the measured explicit-repository
// single-statement file probe. The caller must supply the repository filter.
func scopedCodeTopicFileBranch(termArg int, where string, candidateCap int) string {
	termParam := fmt.Sprintf("term_param AS MATERIALIZED (SELECT $%d::text AS term),\n\t\t  ", termArg)
	contentTerm := "(SELECT term FROM term_param)"
	// #nosec G201 -- termArg and candidateCap are integers; where contains
	// only generated placeholder predicates from codeTopicFilters.
	return fmt.Sprintf(`(
		  WITH %[4]spath_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%%' || $%[1]d || '%%'
		    %[2]s LIMIT %[3]d
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%%' || %[5]s || '%%'
		      AND f.relative_path NOT ILIKE '%%' || $%[1]d || '%%'
		    %[2]s LIMIT (SELECT %[3]d - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)`, termArg, where, candidateCap, termParam, contentTerm)
}
