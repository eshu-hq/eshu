// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"context"
	"strings"
)

// authContextKey is the context key the authenticated request context is stored
// under. It lives here and ONLY here.
//
// If package query kept a second key type of its own, middleware would store
// under one key while a handler family read under another. That compiles, type
// checks, and makes every scoped request read as unauthenticated at runtime,
// with nothing at either call site to show for it.
type authContextKey struct{}

// AuthMode names the source of an authenticated request context.
type AuthMode string

const (
	// AuthModeShared identifies the legacy shared bearer-token path.
	AuthModeShared AuthMode = "shared"
	// AuthModeScoped identifies a token resolved through the scoped registry.
	AuthModeScoped AuthMode = "scoped"
	// AuthModeBrowserSession identifies a server-managed dashboard session.
	AuthModeBrowserSession AuthMode = "browser_session"
)

// AuthContext carries request-scoped authorization bounds for query handlers.
type AuthContext struct {
	Mode                         AuthMode
	TenantID                     string
	WorkspaceID                  string
	SubjectClass                 string
	SubjectIDHash                string
	PolicyRevisionHash           string
	RoleIDs                      []string
	PermissionCatalogEnforced    bool
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	AllScopes                    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	// ExternalProviderConfigID is the stored OIDC/SAML config ID for sessions
	// that were established via an external identity provider. Empty for local
	// password sessions.
	ExternalProviderConfigID string
}

// AuthContextFromContext returns the authenticated request context, if any.
func AuthContextFromContext(ctx context.Context) (AuthContext, bool) {
	if ctx == nil {
		return AuthContext{}, false
	}
	auth, ok := ctx.Value(authContextKey{}).(AuthContext)
	return auth, ok
}

// ContextWithAuthContext returns a child context carrying authorization bounds.
func ContextWithAuthContext(ctx context.Context, auth AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, auth)
}

// CleanedStrings trims, drops empty entries, and de-duplicates while preserving
// order. Auth bounds use it so an allow-list carries no blank or repeated id.
func CleanedStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
