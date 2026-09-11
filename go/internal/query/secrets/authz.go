// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func authorizeSecretsIAMScopedScope(w http.ResponseWriter, r *http.Request, scopeID string) bool {
	filter := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if !filter.Scoped() {
		return true
	}
	if scopeID == "" || !filter.AllowsRepositoryID(scopeID) {
		querycontract.WriteError(w, http.StatusForbidden, "scope is outside the scoped token grant")
		return false
	}
	return true
}
