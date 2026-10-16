// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

// BlockKey is the response data key that carries a walk page's state.
const BlockKey = "rank_walk"

// RowIDKey is the row field a walk keys rows by on both routes.
const RowIDKey = "entity_id"

// Block returns the response rank_walk block for the page. next_rank_cursor
// is absent at the window end.
func (p Page) Block() map[string]any {
	block := map[string]any{
		"page_cursor":       p.PageCursor,
		"served":            p.Served,
		"window_rows":       p.WindowRows,
		"window_size":       WindowSize,
		"window_end":        p.WindowEnd,
		"lexical_truncated": p.LexicalTruncated,
		"excluded_rows":     p.Excluded,
	}
	if p.NextCursor != "" {
		block["next_rank_cursor"] = p.NextCursor
	}
	return block
}

// TrimWalk returns a copy of a decoded rank_walk block for a page cut to
// kept, the first rows of the page in walk order. The copy's
// next_rank_cursor resumes at the first dropped row, served counts only the
// kept rows, and window_end is false. It reports false when the block or a
// kept row cannot prove that cursor (a missing or malformed page cursor, a
// row without a string entity_id); the caller must then refuse the page
// rather than hand out a cursor that would skip rows.
func TrimWalk(walk any, kept []any) (map[string]any, bool) {
	block, ok := walk.(map[string]any)
	if !ok {
		return nil, false
	}
	pageCursor, _ := block["page_cursor"].(string)
	state, err := parseCursor(pageCursor)
	if err != nil {
		return nil, false
	}
	ids := make([]string, 0, len(kept))
	for _, row := range kept {
		fields, isObject := row.(map[string]any)
		id, isString := fields[RowIDKey].(string)
		if !isObject || !isString || id == "" {
			return nil, false
		}
		ids = append(ids, id)
	}
	next, err := Advance(pageCursor, ids)
	if err != nil {
		return nil, false
	}
	trimmed := make(map[string]any, len(block)+1)
	for key, value := range block {
		trimmed[key] = value
	}
	trimmed["next_rank_cursor"] = next
	trimmed["served"] = state.count + len(ids)
	trimmed["window_end"] = false
	return trimmed, true
}
