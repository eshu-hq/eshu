// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// repositoryAccessFilter aliases the querycontract seam type so existing root
// call sites that only name the type (constructing a literal or passing a
// value) keep compiling unchanged. Methods cannot be aliased across packages
// in Go, so call sites invoking a method now call the exported querycontract
// method name directly (e.g. f.Scoped(), f.GraphParams(...)).
type repositoryAccessFilter = querycontract.RepositoryAccessFilter

// repositoryAccessFilterFromContext resolves the request's AuthContext into a
// repositoryAccessFilter. This constructor stays in the root package rather
// than moving into querycontract because it depends on AuthContext,
// AuthContextFromContext, and AuthModeShared (auth.go) — root-package
// concepts used in ~185 other call sites across this package that are out of
// scope for this seam extraction. Moving only this function's body, and
// building the querycontract value through its exported fields, keeps the
// dependency-neutral contract package free of the auth-context type while
// preserving identical behavior.
func repositoryAccessFilterFromContext(ctx context.Context) repositoryAccessFilter {
	auth, ok := AuthContextFromContext(ctx)
	if !ok || auth.AllScopes || auth.Mode == AuthModeShared {
		return repositoryAccessFilter{AllScopes: true}
	}
	allowedScopeIDs := cleanedAuthStrings(auth.AllowedScopeIDs)
	allowedRepositoryIDs := cleanedAuthStrings(auth.AllowedRepositoryIDs)
	allowed := make(map[string]struct{}, len(allowedScopeIDs)+len(allowedRepositoryIDs))
	for _, id := range allowedScopeIDs {
		allowed[id] = struct{}{}
	}
	for _, id := range allowedRepositoryIDs {
		allowed[id] = struct{}{}
	}
	return repositoryAccessFilter{
		AllowedScopeIDs:      allowedScopeIDs,
		AllowedRepositoryIDs: allowedRepositoryIDs,
		Allowed:              allowed,
	}
}

// containsAuthString forwards to querycontract.ContainsAuthString so root call
// sites unrelated to repositoryAccessFilter (e.g. runtime-context grant
// checks) keep using the package-local name.
func containsAuthString(values []string, candidate string) bool {
	return querycontract.ContainsAuthString(values, candidate)
}
