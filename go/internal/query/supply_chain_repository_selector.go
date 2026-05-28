package query

import (
	"net/http"
	"strings"
)

func (h *SupplyChainHandler) resolveSupplyChainRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
) (string, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", true
	}
	repoID, err := resolveRepositorySelectorExact(r.Context(), h.Neo4j, h.Content, selector)
	if err != nil {
		status := http.StatusBadRequest
		if isRepositorySelectorNotFound(err) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}
