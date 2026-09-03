// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import "context"

// PermissionFeatureAskSearch names the permission-catalog feature family that
// gates the ask and curated semantic-search read surfaces.
//
// It lives here rather than beside either handler because those two handlers
// no longer share a package: the semantic-search family moved to
// internal/query/semanticsearch for #6060 while the ask handler stayed in root
// package query. Two copies of the string would authorize against two
// different feature names, and a caller granted one would be denied by the
// other with nothing at either call site to show for it.
const PermissionFeatureAskSearch = "ask_search"

// permissionDataClassesAskSearch is the data-class set a caller must hold in
// full before the ask_search family answers. Stored unexported so callers
// cannot append to the backing array of a shared slice.
var permissionDataClassesAskSearch = []string{
	"ask_reasoning",
	"source_content",
	"documentation_semantic",
}

// PermissionDataClassesAskSearch returns the data classes the ask_search
// feature family requires, as a fresh slice per call. AllowsPermissionDataClasses
// requires every one of them, so a caller holding only some is denied.
func PermissionDataClassesAskSearch() []string {
	return append([]string(nil), permissionDataClassesAskSearch...)
}

// AllowsPermissionFeature reports whether the request's auth context grants
// feature.
//
// It answers true when there is no auth context, when the context does not
// have the permission catalog enforced, or when the caller authenticated on
// the legacy shared bearer path. Those three are the pre-catalog surface: the
// catalog gates callers that carry a derived grant snapshot, and failing them
// closed here would deny every unenforced deployment instead.
//
// A literal "*" in the allow-list grants every feature.
func AllowsPermissionFeature(ctx context.Context, feature string) bool {
	auth, ok := AuthContextFromContext(ctx)
	if !ok || !auth.PermissionCatalogEnforced || auth.Mode == AuthModeShared {
		return true
	}
	for _, allowed := range auth.AllowedPermissionFeatures {
		if allowed == feature || allowed == "*" {
			return true
		}
	}
	return false
}

// AllowsPermissionDataClasses reports whether the request's auth context grants
// every one of dataClasses. It is all-or-nothing on purpose: a partial grant
// would let a handler answer from a class the caller was never given.
//
// The unenforced exits match AllowsPermissionFeature. A literal "*" in the
// allow-list grants every class.
func AllowsPermissionDataClasses(ctx context.Context, dataClasses ...string) bool {
	auth, ok := AuthContextFromContext(ctx)
	if !ok || !auth.PermissionCatalogEnforced || auth.Mode == AuthModeShared {
		return true
	}
	allowed := make(map[string]struct{}, len(auth.AllowedPermissionDataClasses))
	for _, dataClass := range auth.AllowedPermissionDataClasses {
		allowed[dataClass] = struct{}{}
	}
	if _, ok := allowed["*"]; ok {
		return true
	}
	for _, dataClass := range dataClasses {
		if _, ok := allowed[dataClass]; !ok {
			return false
		}
	}
	return true
}
