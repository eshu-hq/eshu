// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
)

func authorizeSecretsIAMScopedScope(w http.ResponseWriter, r *http.Request, scopeID string) bool {
	filter := repositoryAccessFilterFromContext(r.Context())
	if !filter.scoped() {
		return true
	}
	if scopeID == "" || !filter.allowsRepositoryID(scopeID) {
		WriteError(w, http.StatusForbidden, "scope is outside the scoped token grant")
		return false
	}
	return true
}
