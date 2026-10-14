// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fileOperation keeps the db.operation label the unscoped file search has
// always used, so existing dashboards keep matching.
const fileOperation = "search_file_content_any_repo_page"

// The columns of the content_files statements: a hit row carries the file's
// key and metadata with an empty content column; the inner window selects
// content so the outer filter can test the pattern.
const (
	columnsFromContent = `repo_id, relative_path, coalesce(commit_sha, ''), '', content_hash, line_count, coalesce(language, ''), coalesce(artifact_type, '')`
	// columnsFromContentIn is the inner window's column list. It carries the
	// stored lowercase copy instead of the raw content: the outer filter
	// tests only content_lower, so the raw value is never detoasted (#7975).
	columnsFromContentIn = `repo_id, relative_path, commit_sha, content_hash, line_count, language, artifact_type, content_lower`
)

// A step reads one key-ordered window of rows and tests the pattern only on
// the rows it visits. The inner subquery fixes the shape: an ordered
// primary-key walk with LIMIT, never the trigram index, so the cost depends on
// the window and not on the planner's row estimate. The outer filter stops at
// the requested number of matches. The edge branch returns the key of the last
// row of the window; it is absent when the window held fewer rows than asked,
// which means the key space is exhausted.
//
// Arguments: $1 pattern, $2 window rows, $3 matches wanted, then for the
// "after" form $4 cursor repo_id and $5 cursor relative_path.
const stepFirstSQL = `
(SELECT 'hit' AS kind, ` + columnsFromContent + `
 FROM (SELECT ` + columnsFromContentIn + `
       FROM content_files
       ORDER BY repo_id, relative_path
       LIMIT $2::bigint) head
 WHERE content_lower LIKE '%' || lower($1) || '%'
 ORDER BY repo_id, relative_path
 LIMIT $3::bigint)
UNION ALL
(SELECT 'edge', repo_id, relative_path, '', '', '', 0, '', ''
 FROM content_files
 ORDER BY repo_id, relative_path
 OFFSET ($2::bigint - 1) LIMIT 1)`

const stepAfterSQL = `
(SELECT 'hit' AS kind, ` + columnsFromContent + `
 FROM (SELECT ` + columnsFromContentIn + `
       FROM content_files
       WHERE (repo_id, relative_path) > ($4::text, $5::text)
       ORDER BY repo_id, relative_path
       LIMIT $2::bigint) head
 WHERE content_lower LIKE '%' || lower($1) || '%'
 ORDER BY repo_id, relative_path
 LIMIT $3::bigint)
UNION ALL
(SELECT 'edge', repo_id, relative_path, '', '', '', 0, '', ''
 FROM content_files
 WHERE (repo_id, relative_path) > ($4::text, $5::text)
 ORDER BY repo_id, relative_path
 OFFSET ($2::bigint - 1) LIMIT 1)`

// The tail reads every remaining row through the trigram index. It runs with
// index scans disabled so the planner can only choose the trigram bitmap,
// whatever its row estimate says. Arguments: $1 pattern, $2 matches wanted,
// then for the "after" form $3 cursor repo_id and $4 cursor relative_path.
const tailFirstSQL = `
SELECT 'hit' AS kind, ` + columnsFromContent + `
FROM content_files
WHERE content_lower LIKE '%' || lower($1) || '%'
ORDER BY repo_id, relative_path
LIMIT $2::bigint`

const tailAfterSQL = `
SELECT 'hit' AS kind, ` + columnsFromContent + `
FROM content_files
WHERE (repo_id, relative_path) > ($3::text, $4::text)
  AND content_lower LIKE '%' || lower($1) || '%'
ORDER BY repo_id, relative_path
LIMIT $2::bigint`

// fileShape is the content_files table shape: two-column key
// (repo_id, relative_path), querycontract.FileContent rows, and the figures of
// the #7730 measurements.
var fileShape = shape[querycontract.FileContent, querycontract.SearchCursor]{
	table:     "content_files",
	operation: fileOperation,
	subject:   "file content",
	name:      "unscoped search",
	// The file span carries no search.content_kind attribute.
	contentKind:    "",
	partialMessage: "unscoped content search returned a partial result",
	partialEvent:   "content_search.unscoped_partial",
	figures: figures{
		// probeRows and stepRows are the window sizes of the #7730 V4c
		// measurements.
		probeRows: 200,
		stepRows:  500,
		// 0.55 x budget (#7730 continuation shim of 2026-10-11,
		// 7730-continuation-shim-20261011.md, on the QA environment read of
		// 2026-10-11): one key-ordered window to the 11th match of the common
		// tokens that ended partial at 0.15 costs 289-409 ms, so 440 ms reaches
		// them, and the 360 ms left for the tail still covers the measured
		// 268 ms zero-match and 300 ms selective tails. Caps 0.50 to 0.60 pass
		// the model's accuracy rules; 0.65 turns the selective class partial.
		phaseOneCap: 440 * time.Millisecond,
		// 0.5 x budget: the largest tail timeout (#7730 V4c).
		tailCap: 400 * time.Millisecond,
		// The smallest tail timeout worth issuing (#7730 V4c).
		tailFloor: 150 * time.Millisecond,
	},
	stepFirst:      stepFirstSQL,
	stepAfter:      stepAfterSQL,
	tailFirst:      tailFirstSQL,
	tailAfter:      tailAfterSQL,
	countAfter:     countAfterSQL,
	readinessExtra: lowerReadinessSQL,
	keyArgs: func(c querycontract.SearchCursor) []any {
		return []any{c.RepoID, c.RelativePath}
	},
	scan: func(rows db.Rows) (string, querycontract.FileContent, error) {
		var (
			kind string
			file querycontract.FileContent
		)
		err := rows.Scan(&kind, &file.RepoID, &file.RelativePath, &file.CommitSHA, &file.Content,
			&file.ContentHash, &file.LineCount, &file.Language, &file.ArtifactType)
		return kind, file, err
	},
	edgeKey: func(file querycontract.FileContent) querycontract.SearchCursor {
		return querycontract.SearchCursor{RepoID: file.RepoID, RelativePath: file.RelativePath}
	},
}

// filePage turns a finished walk over content_files into the wire page.
func filePage(
	rows []querycontract.FileContent,
	more bool,
	p *partial[querycontract.SearchCursor],
) querycontract.FileSearchPage {
	if p == nil {
		return querycontract.FileSearchPage{Files: rows, More: more}
	}
	return querycontract.FileSearchPage{
		Files: rows,
		Partial: &querycontract.SearchPartial{
			Reason:             p.reason,
			RowsScannedInOrder: p.rowsScannedInOrder,
			RowsMatched:        p.rowsMatched,
			Cursor:             p.cursor,
			BudgetMS:           p.budgetMS,
			ElapsedMS:          p.elapsedMS,
			OverrunMS:          p.overrunMS,
			Progressed:         p.progressed,
			Hint:               p.hint,
		},
		DocumentsNotSearched: p.notSearched,
	}
}
