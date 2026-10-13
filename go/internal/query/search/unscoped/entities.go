// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"math"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// entityOperation keeps the db.operation label the unscoped entity search
// uses, distinct from the file search so dashboards can split them.
const entityOperation = "search_entity_content_any_repo_page"

// These are the statements of the content_entities shape: the same budgeted
// walk over content_entities instead of content_files (#7730). The key order is
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

// entityShape is the content_entities table shape: four-column key
// (repo_id, relative_path, start_line, entity_id), querycontract.EntitySearchHit
// rows that keep their raw metadata, and the figures below.
var entityShape = shape[querycontract.EntitySearchHit, querycontract.SearchCursor]{
	table:          "content_entities",
	operation:      entityOperation,
	subject:        "entity content",
	name:           "unscoped entity search",
	contentKind:    "entity",
	partialMessage: "unscoped entity search returned a partial result",
	partialEvent:   "content_search.unscoped_entity_partial",
	// These are triple A of the #7009 S1 shim, measured on content_entities
	// in the QA environment (docs/internal/evidence/7009-unscoped-entity-search-figures.md
	// repeats the figures it names; the full S1 run is not committed; arbiter
	// ruling round 2, R2.1). Ordered window cost: an ordered
	// window costs 0.3 ms plus 0.34 us per row plus 10.2 ns per byte, so a
	// 1,000-row probe is about 5 ms and a 5,000-row step 18-43 ms, and
	// 160 ms (0.20 x budget) walks about 35,000 rows; smaller windows lose
	// decode at limit 200 to per-window round trips. Tail cost: the rare
	// bitmap tails cost 400-462 ms warm (517-578 ms cold), so the tail cap is
	// 0.80 x budget (640 ms); the 13.1 MB value's recheck blocks a tail
	// cancel that fires 30-170 ms into the tail, and with these figures the
	// default tail timeout never lands there. Model: these figures keep
	// every class that is exact today exact at limit 10, warm and cold, with
	// a 1.25 cost margin; the file figures turn process_entries and
	// FeaturesTab partial. The one disclosed trade (R2.2): a decode-like
	// common pattern at limit 200 is a partial of about 149 rows that one
	// resume completes. TestEntityPolicyClasses pins every class.
	figures: figures{
		probeRows:   1000,
		stepRows:    5000,
		phaseOneCap: 160 * time.Millisecond,
		tailCap:     640 * time.Millisecond,
		tailFloor:   150 * time.Millisecond,
	},
	stepFirst: entityStepFirstSQL,
	stepAfter: entityStepAfterSQL,
	tailFirst: entityTailFirstSQL,
	tailAfter: entityTailAfterSQL,
	keyArgs: func(c querycontract.SearchCursor) []any {
		return []any{c.RepoID, c.RelativePath, saturateCursorStartLine(c.StartLine), c.EntityID}
	},
	// A match keeps its raw metadata beside the row; the query layer decodes
	// it through its one metadata decode.
	scan: func(rows db.Rows) (string, querycontract.EntitySearchHit, error) {
		var (
			kind string
			hit  querycontract.EntitySearchHit
		)
		err := rows.Scan(&kind, &hit.Entity.EntityID, &hit.Entity.RepoID, &hit.Entity.RelativePath,
			&hit.Entity.EntityType, &hit.Entity.EntityName, &hit.Entity.StartLine, &hit.Entity.EndLine,
			&hit.Entity.Language, &hit.Entity.SourceCache, &hit.MetadataRaw)
		return kind, hit, err
	},
	edgeKey: func(hit querycontract.EntitySearchHit) querycontract.SearchCursor {
		return querycontract.SearchCursor{
			RepoID:       hit.Entity.RepoID,
			RelativePath: hit.Entity.RelativePath,
			StartLine:    hit.Entity.StartLine,
			EntityID:     hit.Entity.EntityID,
		}
	},
}

// saturateCursorStartLine clamps a cursor start line to the int32 range of
// the start_line column. A bare int32 conversion wraps out-of-range values:
// math.MaxInt64 becomes -1, which sorts before every row and resumes the
// walk from the wrong position, returning rows the caller already has.
// Saturation keeps the key-order comparison on the correct side of the key
// space: above MaxInt32 sorts past every storable row, below MinInt32 sorts
// before the first one.
func saturateCursorStartLine(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	// #nosec G115 -- v is clamped to the int32 range above before narrowing.
	return int32(v)
}

// entityPage turns a finished walk over content_entities into an EntityPage.
func entityPage(
	hits []querycontract.EntitySearchHit,
	more bool,
	p *partial[querycontract.SearchCursor],
) EntityPage {
	if p == nil {
		return EntityPage{Hits: hits, More: more}
	}
	return EntityPage{Hits: hits, Partial: &querycontract.SearchPartial{
		Reason:             p.reason,
		RowsScannedInOrder: p.rowsScannedInOrder,
		RowsMatched:        p.rowsMatched,
		Cursor:             p.cursor,
		BudgetMS:           p.budgetMS,
		ElapsedMS:          p.elapsedMS,
		OverrunMS:          p.overrunMS,
		Progressed:         p.progressed,
		Hint:               p.hint,
	}}
}
