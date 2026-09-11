// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package readmodel

import (
	"net/http"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Paging bounds for the repository list route. ListPageFromRequest clamps
// an out-of-range or unset `limit`/`offset` query parameter to these values
// rather than rejecting the request.
const (
	// ListDefaultLimit is the page size used when the request omits `limit`
	// or supplies a non-positive value.
	ListDefaultLimit = 100
	// ListMaxLimit is the largest page size a caller may request; a larger
	// `limit` is clamped down to it.
	ListMaxLimit = 500
	// ListMaxOffset is the largest `offset` a caller may request; a larger
	// offset is clamped down to it rather than paging past it.
	ListMaxOffset = 10000
)

// ListPage is the resolved limit/offset window for one repository list
// request, after ListPageFromRequest has clamped both to their bounds.
type ListPage struct {
	Limit  int
	Offset int
}

// ListPageFromRequest parses the `limit` and `offset` query parameters off
// r and clamps each to its bound (ListDefaultLimit/ListMaxLimit for limit,
// 0/ListMaxOffset for offset). A missing, non-positive, or over-limit value
// never errors; it silently resolves to the nearest in-range value.
func ListPageFromRequest(r *http.Request) ListPage {
	limit := querycontract.QueryParamInt(r, "limit", ListDefaultLimit)
	if limit <= 0 {
		limit = ListDefaultLimit
	}
	if limit > ListMaxLimit {
		limit = ListMaxLimit
	}
	offset := querycontract.QueryParamInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > ListMaxOffset {
		offset = ListMaxOffset
	}
	return ListPage{Limit: limit, Offset: offset}
}

// ListResponse builds the standard paged repository envelope. total
// is the true repository count independent of the page size; count is the
// number of rows returned in this page. Callers that do not yet have a total
// (e.g. early-return paths) may pass total=len(repos) and rely on the caller
// to patch it once the count query resolves.
func ListResponse(repos []map[string]any, page ListPage, truncated bool, total int) map[string]any {
	return map[string]any{
		"repositories": repos,
		"count":        len(repos),
		"total":        total,
		"limit":        page.Limit,
		"offset":       page.Offset,
		"truncated":    truncated,
	}
}

// PageRepositoryMaps sorts repos by (name, id) for stable pagination, then
// slices out the window page describes. It returns the sliced page and
// whether repos held more rows past that window (truncated). repos is
// sorted in place; page.Offset at or past len(repos) returns an empty page,
// never an error.
func PageRepositoryMaps(repos []map[string]any, page ListPage) ([]map[string]any, bool) {
	sort.SliceStable(repos, func(i, j int) bool {
		leftName, rightName := querycontract.StringVal(repos[i], "name"), querycontract.StringVal(repos[j], "name")
		if leftName != rightName {
			return leftName < rightName
		}
		return querycontract.StringVal(repos[i], "id") < querycontract.StringVal(repos[j], "id")
	})
	if page.Offset >= len(repos) {
		return []map[string]any{}, false
	}
	end := page.Offset + page.Limit
	truncated := end < len(repos)
	if end > len(repos) {
		end = len(repos)
	}
	return repos[page.Offset:end], truncated
}
