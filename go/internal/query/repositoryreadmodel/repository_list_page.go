// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryreadmodel

import (
	"net/http"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	RepositoryListDefaultLimit = 100
	RepositoryListMaxLimit     = 500
	RepositoryListMaxOffset    = 10000
)

type RepositoryListPage struct {
	Limit  int
	Offset int
}

func RepositoryListPageFromRequest(r *http.Request) RepositoryListPage {
	limit := querycontract.QueryParamInt(r, "limit", RepositoryListDefaultLimit)
	if limit <= 0 {
		limit = RepositoryListDefaultLimit
	}
	if limit > RepositoryListMaxLimit {
		limit = RepositoryListMaxLimit
	}
	offset := querycontract.QueryParamInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > RepositoryListMaxOffset {
		offset = RepositoryListMaxOffset
	}
	return RepositoryListPage{Limit: limit, Offset: offset}
}

// RepositoryListResponse builds the standard paged repository envelope. total
// is the true repository count independent of the page size; count is the
// number of rows returned in this page. Callers that do not yet have a total
// (e.g. early-return paths) may pass total=len(repos) and rely on the caller
// to patch it once the count query resolves.
func RepositoryListResponse(repos []map[string]any, page RepositoryListPage, truncated bool, total int) map[string]any {
	return map[string]any{
		"repositories": repos,
		"count":        len(repos),
		"total":        total,
		"limit":        page.Limit,
		"offset":       page.Offset,
		"truncated":    truncated,
	}
}

func PageRepositoryMaps(repos []map[string]any, page RepositoryListPage) ([]map[string]any, bool) {
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
