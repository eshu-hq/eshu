// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// permissionFeatureAskSearch is the ask/semantic-search feature family.
	// The value lives in queryauth so the semantic-search family, which moved
	// to internal/query/semanticsearch for #6060, authorizes against the same
	// name this package's ask handler does.
	permissionFeatureAskSearch     = queryauth.PermissionFeatureAskSearch
	permissionFeatureAuditExport   = queryauth.PermissionFeatureAuditExport
	permissionFeatureIdentityAdmin = queryauth.PermissionFeatureIdentityAdmin
	permissionFeatureRolesGrants   = queryauth.PermissionFeatureRolesGrants
	permissionFeatureTokens        = queryauth.PermissionFeatureTokens
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

// requirePermissionFeature forwards to querycontract.RequirePermissionFeature.
// It lives there (#6642), not queryauth, because it needs querycontract's
// own WriteJSON/ResponseEnvelope/ErrorEnvelope primitives: queryauth cannot
// import querycontract (querycontract already imports queryauth, and the
// reverse edge would cycle).
func requirePermissionFeature(w http.ResponseWriter, r *http.Request, capability string, feature string) bool {
	return querycontract.RequirePermissionFeature(w, r, capability, feature)
}

// writePermissionDeniedEnvelope forwards to querycontract.WritePermissionDenied.
// See requirePermissionFeature's comment for why it lives in querycontract.
func writePermissionDeniedEnvelope(w http.ResponseWriter, capability string) {
	querycontract.WritePermissionDenied(w, capability)
}
