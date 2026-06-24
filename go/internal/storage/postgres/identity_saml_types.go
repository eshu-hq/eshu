package postgres

import "time"

// SAMLExternalSubjectResolutionRequest identifies one hash-only SAML subject
// and its current group-claim digest for durable authorization resolution.
type SAMLExternalSubjectResolutionRequest struct {
	ProviderConfigID      string
	ExternalSubjectIDHash string
	GroupClaimsHash       string
	Now                   time.Time
}

// SAMLExternalSubjectAuthContext is the durable authorization context resolved
// from identity provider, subject, membership, role, and grant state.
//
// RoleIDs, PermissionCatalogEnforced, AllowedPermissionFeatures, and
// AllowedPermissionDataClasses carry the same permission-catalog snapshot a
// scoped token for the same roles would carry, so a SAML cookie session
// enforces identically to a local or OIDC session for the same roles. They
// are populated only for non-all-scope (non-admin) sessions; all-scope sessions
// keep PermissionCatalogEnforced=false and remain fail-open.
type SAMLExternalSubjectAuthContext struct {
	TenantID                     string
	WorkspaceID                  string
	SubjectIDHash                string
	SubjectClass                 string
	PolicyRevisionHash           string
	AllScopes                    bool
	RoleIDs                      []string
	PermissionCatalogEnforced    bool
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
}

// SAMLExternalSubjectResolutionResult distinguishes unmapped subjects from
// known subjects that were denied by current group, membership, role, or grant
// state so callers can fail closed instead of using bootstrap fallbacks.
type SAMLExternalSubjectResolutionResult struct {
	Auth         SAMLExternalSubjectAuthContext
	Resolved     bool
	KnownSubject bool
}
