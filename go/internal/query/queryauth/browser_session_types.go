// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"context"
	"time"
)

// BrowserSessionStore is the write surface for server-managed dashboard
// sessions. Implementations must persist only hashed session and CSRF
// values. Moved from root package query's browser_session_handler.go
// (#6642) so a handler-family subpackage can name it without importing
// root.
type BrowserSessionStore interface {
	CreateBrowserSession(context.Context, BrowserSessionCreateRecord) error
	RevokeBrowserSession(context.Context, string, time.Time) error
	SwitchBrowserSessionWorkspace(context.Context, string, string, string, time.Time) (AuthContext, bool, error)
}

// BrowserSessionCreateRecord is the hash-only session row requested by the
// HTTP handler. Moved alongside BrowserSessionStore.
type BrowserSessionCreateRecord struct {
	SessionHash                  string
	CSRFTokenHash                string
	TenantID                     string
	WorkspaceID                  string
	SubjectIDHash                string
	SubjectClass                 string
	PolicyRevisionHash           string
	RoleIDs                      []string
	AllScopes                    bool
	PermissionCatalogEnforced    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	ExternalProviderConfigID     string
	ExternalSubjectIDHash        string
	ExternalGroupHashes          []string
	ExternalAuthValidatedAt      time.Time
	ExternalAuthStaleAfter       time.Time
	IssuedAt                     time.Time
	LastSeenAt                   time.Time
	IdleExpiresAt                time.Time
	AbsoluteExpiresAt            time.Time
	UpdatedAt                    time.Time
}

// BrowserSessionResponse is returned by browser session routes. Moved from
// root package query's browser_session_handler.go (#6642).
type BrowserSessionResponse struct {
	Auth              BrowserSessionAuthResponse `json:"auth"`
	CSRFToken         string                     `json:"csrf_token,omitempty"`
	IdleExpiresAt     time.Time                  `json:"idle_expires_at,omitempty"`
	AbsoluteExpiresAt time.Time                  `json:"absolute_expires_at,omitempty"`
}

// BrowserSessionAuthResponse is the public JSON view of a request auth
// context. Moved from root package query's browser_session_handler.go
// (#6642).
type BrowserSessionAuthResponse struct {
	Mode                      AuthMode `json:"mode"`
	TenantID                  string   `json:"tenant_id,omitempty"`
	WorkspaceID               string   `json:"workspace_id,omitempty"`
	SubjectClass              string   `json:"subject_class,omitempty"`
	SubjectIDHash             string   `json:"subject_id_hash,omitempty"`
	PolicyRevisionHash        string   `json:"policy_revision_hash,omitempty"`
	RoleIDs                   []string `json:"role_ids,omitempty"`
	AllScopes                 bool     `json:"all_scopes"`
	AllowedScopeIDs           []string `json:"allowed_scope_ids,omitempty"`
	AllowedRepositoryIDs      []string `json:"allowed_repository_ids,omitempty"`
	PermissionCatalogEnforced bool     `json:"permission_catalog_enforced"`
	AllowedPermissionFeatures []string `json:"allowed_permission_features,omitempty"`
	// ExternalProviderConfigID is the stored OIDC/SAML provider config ID for
	// sessions established via an external IdP. Omitted for local sessions.
	ExternalProviderConfigID string `json:"external_provider_config_id,omitempty"`
}

// NormalizeBrowserSessionAuthContext normalizes auth the same way
// NormalizeAuthContext does, and additionally promotes a scoped mode to
// AuthModeBrowserSession. Moved from root package query's auth.go
// (normalizeBrowserSessionAuthContext, #6642).
func NormalizeBrowserSessionAuthContext(auth AuthContext) AuthContext {
	auth = NormalizeAuthContext(auth)
	if auth.Mode == AuthModeScoped {
		auth.Mode = AuthModeBrowserSession
	}
	return auth
}

// BrowserSessionAuthResponseFor builds the public JSON view of auth for a
// browser session response. Moved from root package query's
// browser_session_handler.go (browserSessionAuthResponse, #6642) and renamed
// to avoid colliding with the BrowserSessionAuthResponse type this package
// also exports -- root's unexported spelling capitalized to plain
// BrowserSessionAuthResponse would have collided with the type of the same
// name.
func BrowserSessionAuthResponseFor(auth AuthContext) BrowserSessionAuthResponse {
	auth = NormalizeBrowserSessionAuthContext(auth)
	return BrowserSessionAuthResponse{
		Mode:                      auth.Mode,
		TenantID:                  auth.TenantID,
		WorkspaceID:               auth.WorkspaceID,
		SubjectClass:              auth.SubjectClass,
		SubjectIDHash:             auth.SubjectIDHash,
		PolicyRevisionHash:        auth.PolicyRevisionHash,
		RoleIDs:                   append([]string(nil), auth.RoleIDs...),
		AllScopes:                 auth.AllScopes,
		AllowedScopeIDs:           append([]string(nil), auth.AllowedScopeIDs...),
		AllowedRepositoryIDs:      append([]string(nil), auth.AllowedRepositoryIDs...),
		PermissionCatalogEnforced: auth.PermissionCatalogEnforced,
		AllowedPermissionFeatures: append([]string(nil), auth.AllowedPermissionFeatures...),
		ExternalProviderConfigID:  auth.ExternalProviderConfigID,
	}
}
