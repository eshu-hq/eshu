// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// WritePermissionDenied writes the standard 403 permission-denied envelope
// for capability. Moved from root package query's permission_catalog.go
// (writePermissionDeniedEnvelope, #6642) so a handler-family subpackage can
// write the same envelope without importing root. It lives here rather than
// in queryauth because it depends on this package's own WriteJSON,
// ResponseEnvelope, ErrorEnvelope, and ErrorCodePermissionDenied: queryauth
// cannot import querycontract (querycontract already imports queryauth for
// RepositoryAccessFilterFromContext, and the reverse edge would cycle).
func WritePermissionDenied(w http.ResponseWriter, capability string) {
	WriteJSON(w, http.StatusForbidden, ResponseEnvelope{Error: &ErrorEnvelope{
		Code:       ErrorCodePermissionDenied,
		Message:    "permission denied",
		Capability: capability,
	}})
}

// RequirePermissionFeature reports whether the request's auth context grants
// feature, writing the WritePermissionDenied envelope and returning false
// when it does not. Moved from root package query's permission_catalog.go
// (requirePermissionFeature, #6642); same signature as root's, body
// unchanged apart from calling queryauth.AllowsPermissionFeature and
// WritePermissionDenied directly instead of through root's forwarders.
func RequirePermissionFeature(w http.ResponseWriter, r *http.Request, capability string, feature string) bool {
	if queryauth.AllowsPermissionFeature(r.Context(), feature) {
		return true
	}
	WritePermissionDenied(w, capability)
	return false
}
