package query

import (
	"context"
	"net/http"
)

func (h *CodeHandler) applyRepositorySelector(w http.ResponseWriter, r *http.Request, selector *string) bool {
	if selector == nil {
		return true
	}
	resolved, err := h.resolveRepositorySelector(r.Context(), *selector)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return false
	}
	*selector = resolved
	return true
}

func (h *CodeHandler) resolveRepositorySelector(ctx context.Context, selector string) (string, error) {
	return resolveRepositorySelectorExact(ctx, h.Neo4j, h.Content, selector)
}
