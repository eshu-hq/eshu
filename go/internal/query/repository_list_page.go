package query

import (
	"net/http"
	"sort"
)

const (
	repositoryListDefaultLimit = 100
	repositoryListMaxLimit     = 500
	repositoryListMaxOffset    = 10000
)

type repositoryListPage struct {
	Limit  int
	Offset int
}

func repositoryListPageFromRequest(r *http.Request) repositoryListPage {
	limit := QueryParamInt(r, "limit", repositoryListDefaultLimit)
	if limit <= 0 {
		limit = repositoryListDefaultLimit
	}
	if limit > repositoryListMaxLimit {
		limit = repositoryListMaxLimit
	}
	offset := QueryParamInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > repositoryListMaxOffset {
		offset = repositoryListMaxOffset
	}
	return repositoryListPage{Limit: limit, Offset: offset}
}

// repositoryListResponse builds the standard paged repository envelope. total
// is the true repository count independent of the page size; count is the
// number of rows returned in this page. Callers that do not yet have a total
// (e.g. early-return paths) may pass total=len(repos) and rely on the caller
// to patch it once the count query resolves.
func repositoryListResponse(repos []map[string]any, page repositoryListPage, truncated bool, total int) map[string]any {
	return map[string]any{
		"repositories": repos,
		"count":        len(repos),
		"total":        total,
		"limit":        page.Limit,
		"offset":       page.Offset,
		"truncated":    truncated,
	}
}

func pageRepositoryMaps(repos []map[string]any, page repositoryListPage) ([]map[string]any, bool) {
	sort.SliceStable(repos, func(i, j int) bool {
		leftName, rightName := StringVal(repos[i], "name"), StringVal(repos[j], "name")
		if leftName != rightName {
			return leftName < rightName
		}
		return StringVal(repos[i], "id") < StringVal(repos[j], "id")
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
