// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"errors"
	"fmt"
	"strings"
)

// MapRow builds a window Row from a decoded result row: the id is the
// RowIDKey field, and the content joins the named fields in order, each
// rendered with fmt and keyed by name, so a change to any of them, or a
// field appearing or disappearing, changes the window basis.
func MapRow(fields map[string]any, contentKeys ...string) Row {
	id, _ := fields[RowIDKey].(string)
	var content strings.Builder
	for _, key := range contentKeys {
		value, present := fields[key]
		fmt.Fprintf(&content, "%s=%t:%v\x00", key, present, value)
	}
	return Row{ID: id, Content: content.String()}
}

// MapRows returns the window rows for decoded result rows (see MapRow) and
// an index of the first row for each id, for MapPage.
func MapRows(rows []map[string]any, contentKeys ...string) ([]Row, map[string]map[string]any) {
	windowRows := make([]Row, 0, len(rows))
	byID := make(map[string]map[string]any, len(rows))
	for _, fields := range rows {
		row := MapRow(fields, contentKeys...)
		windowRows = append(windowRows, row)
		if _, seen := byID[row.ID]; !seen {
			byID[row.ID] = fields
		}
	}
	return windowRows, byID
}

// ErrPageRowMissing reports a page id with no row to serve. Walk counts every
// page id as served (Position, NextCursor), so serving a page without it
// would lose that row from the walk. Callers answer it as a server fault.
var ErrPageRowMissing = errors.New("rank walk: page id has no row")

// MapPage returns the page's rows in walk order from byID. A row Scored by
// scores gets search_backend="hybrid"; every row loses page_position, because
// a walk page is not a window of the offset order. The rows are modified in
// place and must be request-local. MapPage refuses a page id with no row (or
// a nil row) with ErrPageRowMissing and no rows, before it modifies any row:
// it never writes to a nil map and never returns a short page.
func MapPage(byID map[string]map[string]any, scores map[string]float64, ids []string) ([]map[string]any, error) {
	page := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok || row == nil {
			return nil, fmt.Errorf("%w: %q", ErrPageRowMissing, id)
		}
		page = append(page, row)
	}
	for i, row := range page {
		delete(row, "page_position")
		if Scored(scores, ids[i]) {
			row["search_backend"] = "hybrid"
		}
	}
	return page, nil
}
