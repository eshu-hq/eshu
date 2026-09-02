// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

const (
	// permissionFeatureAskSearch is the ask/semantic-search feature family.
	// The value lives in queryauth so the semantic-search family, which moved
	// to internal/query/semanticsearch for #6060, authorizes against the same
	// name this package's ask handler does.
	permissionFeatureAskSearch     = queryauth.PermissionFeatureAskSearch
	permissionFeatureAuditExport   = "audit_export"
	permissionFeatureIdentityAdmin = "identity_admin"
	permissionFeatureRolesGrants   = "roles_grants"
	permissionFeatureTokens        = "tokens"
)

// permissionDataClassesAskSearch is the data-class set the ask_search family
// requires. queryauth owns the list for the same reason it owns the feature
// name.
var permissionDataClassesAskSearch = queryauth.PermissionDataClassesAskSearch()

func authContextAllowsPermissionFeature(ctx context.Context, feature string) bool {
	return queryauth.AllowsPermissionFeature(ctx, feature)
}

func authContextAllowsPermissionDataClasses(ctx context.Context, dataClasses ...string) bool {
	return queryauth.AllowsPermissionDataClasses(ctx, dataClasses...)
}

func requirePermissionFeature(w http.ResponseWriter, r *http.Request, capability string, feature string) bool {
	if authContextAllowsPermissionFeature(r.Context(), feature) {
		return true
	}
	writePermissionDeniedEnvelope(w, capability)
	return false
}

func writePermissionDeniedEnvelope(w http.ResponseWriter, capability string) {
	WriteJSON(w, http.StatusForbidden, ResponseEnvelope{Error: &ErrorEnvelope{
		Code:       ErrorCodePermissionDenied,
		Message:    "permission denied",
		Capability: capability,
	}})
}
