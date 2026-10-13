// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

// This file is the entity twin of statements.go: the same budgeted walk over
// content_entities instead of content_files (#7730). The key order is
// (repo_id, relative_path, start_line, entity_id) — the walk-order index
// content_entities_repo_path_start_idx serves it — and the keyset predicate
// carries all four parts because several entities share one file: resuming on
// the file key alone would return the cursor's filemates twice. The hit
// projection carries the payload the old single statement returned
// (source_cache and metadata), so an exact page equals the old page row for
// row; the file walk omits bodies, the entity walk cannot.
//
// Arguments mirror the file statements with the longer key: a step takes $1
// pattern, $2 window rows, $3 matches wanted, then for the "after" form $4
// cursor repo_id, $5 cursor relative_path, $6 cursor start_line, $7 cursor
// entity_id. A tail takes $1 pattern, $2 matches wanted, then for the "after"
// form $3/$4/$5/$6.
const (
	entityColumnsFromContent = `entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''), metadata`
	entityColumnsIn          = `entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata`
	entityKeyOrder           = `repo_id, relative_path, start_line, entity_id`
	entityKeysetAfter        = `(repo_id, relative_path, start_line, entity_id) > ($4::text, $5::text, $6::integer, $7::text)`
	entityTailKeysetAfter    = `(repo_id, relative_path, start_line, entity_id) > ($3::text, $4::text, $5::integer, $6::text)`
	entityEdgeDefaults       = `'edge', entity_id, repo_id, relative_path, '', '', start_line, 0, '', '', NULL`
)

const entityStepFirstSQL = `
(SELECT 'hit' AS kind, ` + entityColumnsFromContent + `
 FROM (SELECT ` + entityColumnsIn + `
       FROM content_entities
       ORDER BY ` + entityKeyOrder + `
       LIMIT $2::bigint) head
 WHERE source_cache ILIKE '%' || $1 || '%'
 ORDER BY ` + entityKeyOrder + `
 LIMIT $3::bigint)
UNION ALL
(SELECT ` + entityEdgeDefaults + `
 FROM content_entities
 ORDER BY ` + entityKeyOrder + `
 OFFSET ($2::bigint - 1) LIMIT 1)`

const entityStepAfterSQL = `
(SELECT 'hit' AS kind, ` + entityColumnsFromContent + `
 FROM (SELECT ` + entityColumnsIn + `
       FROM content_entities
       WHERE ` + entityKeysetAfter + `
       ORDER BY ` + entityKeyOrder + `
       LIMIT $2::bigint) head
 WHERE source_cache ILIKE '%' || $1 || '%'
 ORDER BY ` + entityKeyOrder + `
 LIMIT $3::bigint)
UNION ALL
(SELECT ` + entityEdgeDefaults + `
 FROM content_entities
 WHERE ` + entityKeysetAfter + `
 ORDER BY ` + entityKeyOrder + `
 OFFSET ($2::bigint - 1) LIMIT 1)`

const entityTailFirstSQL = `
SELECT 'hit' AS kind, ` + entityColumnsFromContent + `
FROM content_entities
WHERE source_cache ILIKE '%' || $1 || '%'
ORDER BY ` + entityKeyOrder + `
LIMIT $2::bigint`

const entityTailAfterSQL = `
SELECT 'hit' AS kind, ` + entityColumnsFromContent + `
FROM content_entities
WHERE ` + entityTailKeysetAfter + `
  AND source_cache ILIKE '%' || $1 || '%'
ORDER BY ` + entityKeyOrder + `
LIMIT $2::bigint`

// EntityStatementSet is the shipped text of the four bounded entity
// statements, exported so the live plan-shape and differential proofs derive
// their statements from the constants production runs instead of copying them.
type EntityStatementSet struct {
	// StepFirst and StepAfter are the key-ordered window statements for the
	// first window and for a window after a cursor.
	StepFirst, StepAfter string
	// TailFirst and TailAfter are the trigram tail statements.
	TailFirst, TailAfter string
	// DisableIndexScan is the SET LOCAL the tail runs under.
	DisableIndexScan string
}

// EntityStatements returns the shipped entity statement text.
func EntityStatements() EntityStatementSet {
	return EntityStatementSet{
		StepFirst:        entityStepFirstSQL,
		StepAfter:        entityStepAfterSQL,
		TailFirst:        entityTailFirstSQL,
		TailAfter:        entityTailAfterSQL,
		DisableIndexScan: disableIndexScanSQL,
	}
}
