// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// RepositoryAccessFilterFromContext builds the repository-access bounds for a
// request from its authenticated context.
//
// This lived in the root query package until #6060, with a comment explaining
// that it could not move because it depends on AuthContext,
// AuthContextFromContext and AuthModeShared, which were root concepts. Those all
// moved to queryauth, a standard-library-only type leaf, so the reason no longer
// holds and the constructor now sits beside the type it constructs.
//
// A caller with no grants must come back Empty rather than AllScopes. The
// AllScopes shortcut is reached only for an unauthenticated context, an
// explicit all-scopes grant, or the legacy shared-token mode.
func RepositoryAccessFilterFromContext(ctx context.Context) RepositoryAccessFilter {
	auth, ok := queryauth.AuthContextFromContext(ctx)
	if !ok || auth.AllScopes || auth.Mode == queryauth.AuthModeShared {
		return RepositoryAccessFilter{AllScopes: true}
	}
	allowedScopeIDs := queryauth.CleanedStrings(auth.AllowedScopeIDs)
	allowedRepositoryIDs := queryauth.CleanedStrings(auth.AllowedRepositoryIDs)
	allowed := make(map[string]struct{}, len(allowedScopeIDs)+len(allowedRepositoryIDs))
	for _, id := range allowedScopeIDs {
		allowed[id] = struct{}{}
	}
	for _, id := range allowedRepositoryIDs {
		allowed[id] = struct{}{}
	}
	return RepositoryAccessFilter{
		AllowedScopeIDs:      allowedScopeIDs,
		AllowedRepositoryIDs: allowedRepositoryIDs,
		Allowed:              allowed,
	}
}
