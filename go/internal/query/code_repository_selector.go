// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
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

// codeContentGrantScope resolves the caller's repository grant for a code read
// that is optionally anchored to one repository by repoID.
//
// It exists because applyRepositorySelectorForCapability only binds a grant to
// a selector the caller actually supplied: queryselector.ResolveExactForAccess
// returns "" for an empty selector without consulting the grant at all
// (queryselector/selector.go), so every code route that treats an omitted
// repo_id as "search everything" ran its downstream query with no grant bound.
//
// allowed is the granted repository id list to push into the read's own
// predicate for a corpus-wide search -- nil when the caller is unscoped (the
// shared/admin/local read stays unrestricted) or when the read is already
// anchored to a single granted repository. blocked reports that the caller's
// grant admits nothing, so the route must return its empty page WITHOUT
// touching the store: an empty id list reads as "unrestricted" to every
// `repo_id = ANY($n)` / `id IN $allowed_repository_ids` predicate in this
// package, which is exactly how a grantless scoped caller would otherwise see
// the whole corpus.
func codeContentGrantScope(ctx context.Context, repoID string) (allowed []string, blocked bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return nil, true
	}
	if !access.Scoped() {
		return nil, false
	}
	if repoID = strings.TrimSpace(repoID); repoID != "" {
		// Defense in depth: the selector already resolved repoID through the
		// grant, so a mismatch here means a caller reached the read on a
		// selector-free path.
		return nil, !access.AllowsRepositoryID(repoID)
	}
	return access.RepositorySearchIDs(), false
}
