// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
)

// applyRepositorySelectorForCapability resolves *selector, and on failure
// writes the response and reports false. Selector resolution issues its own
// graph read, so a Neo4jReader timeout or outage is mapped to the shared
// 503/504 bounded graph-read contract using capability rather than falling
// through to the generic 400 branch, which would tell the client its request
// was malformed during a purely transient backend condition.
func (h *CodeHandler) applyRepositorySelectorForCapability(w http.ResponseWriter, r *http.Request, selector *string, capability string) bool {
	if selector == nil {
		return true
	}
	resolved, err := h.resolveRepositorySelector(r.Context(), *selector)
	if err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return false
		}
		WriteError(w, http.StatusBadRequest, err.Error())
		return false
	}
	*selector = resolved
	return true
}

func (h *CodeHandler) resolveRepositorySelector(ctx context.Context, selector string) (string, error) {
	return resolveRepositorySelectorExactForAccess(
		ctx,
		h.Neo4j,
		h.Content,
		selector,
		repositoryAccessFilterFromContext(ctx),
	)
}
