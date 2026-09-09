// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// InvitationListItem is the metadata-only invitation view returned to a
// tenant admin. It never carries an invite code, invitee handle, or inviter
// identity (those are stored only as hashes and are never read back).
type InvitationListItem struct {
	InviteID    string
	RoleID      string
	Status      string
	ExpiresAt   time.Time
	AcceptedAt  time.Time
	RevokedAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TenantID    string
	WorkspaceID string
}

// RoleAssignmentListItem is the metadata-only membership-role assignment
// view returned to a tenant admin.
type RoleAssignmentListItem struct {
	UserID           string
	RoleID           string
	AssignmentSource string
	Status           string
	EffectiveAt      time.Time
	ExpiresAt        time.Time
	TenantID         string
	WorkspaceID      string
}

// RoleGrantListItem is one capability grant attached to a role.
type RoleGrantListItem struct {
	GrantID    string
	Action     string
	Feature    string
	DataClass  string
	ScopeClass string
	Status     string
}

// RoleListItem is a tenant role plus the grants it confers, returned to a
// tenant admin so they can see what each role permits.
type RoleListItem struct {
	RoleID  string
	Status  string
	BuiltIn bool
	Grants  []RoleGrantListItem
}

// IdPProviderListItem is the metadata-only identity-provider view returned
// to a tenant admin. It never carries issuer/metadata/entity/client hashes or
// credential handles.
type IdPProviderListItem struct {
	ProviderConfigID string
	ProviderKind     string
	Status           string
}

// IdPGroupMappingListItem is the metadata-only group->role mapping view
// returned to a tenant admin. MappingRef is an opaque, non-secret reference; the
// hashed external group name is never returned.
type IdPGroupMappingListItem struct {
	MappingRef       string
	ProviderConfigID string
	RoleID           string
	Status           string
	EffectiveAt      time.Time
	ExpiresAt        time.Time
	TenantID         string
	WorkspaceID      string
}

// APITokenListItem is the metadata-only generated-token view returned to a
// tenant admin across all users. It never carries token_hash or display label
// hashes. DisplayLabel (issue #3708) is the real, non-secret operator-facing
// label and is safe to return as-is.
type APITokenListItem struct {
	TokenID            string
	TokenClass         string
	UserID             string
	ServicePrincipalID string
	Status             string
	DisplayLabel       string
	IssuedAt           time.Time
	ExpiresAt          time.Time
	RevokedAt          time.Time
	TenantID           string
	WorkspaceID        string
}

// AuditQuery bounds an admin audit-event read. OperatorAuthorized is set by
// the handler only after the authorization gate passes; the underlying store
// refuses to return detailed events when it is false.
type AuditQuery struct {
	OperatorAuthorized bool
	EventType          string
	Decision           string
	ReasonCode         string
	OccurredAfter      time.Time
	OccurredBefore     time.Time
	Limit              int
	// OrderDesc requests most-recent-first ordering (occurred_at DESC). The
	// admin read surface always sets this true so a bounded page shows the
	// newest events, not the oldest.
	OrderDesc bool
	// TenantID constrains the query to events belonging to this tenant. When
	// set the store applies a tenant_id = $N predicate and global/NULL-tenant
	// events are excluded. Leave empty for a shared-operator query that should
	// see all events regardless of tenant.
	TenantID string
}

// ReadStore is the read surface the admin console backend uses for
// tenant-scoped identity metadata. Every method is scoped strictly to the
// caller's tenant (and workspace where applicable); none returns a secret,
// hash, invite code, credential handle, or external group hash.
type ReadStore interface {
	// ListAdminInvitations returns invitations in the tenant/workspace.
	ListAdminInvitations(ctx context.Context, tenantID, workspaceID string) ([]InvitationListItem, error)
	// ListAdminRoleAssignments returns membership-role assignments in the
	// tenant/workspace, optionally filtered by userID (blank lists all).
	ListAdminRoleAssignments(ctx context.Context, tenantID, workspaceID, userID string) ([]RoleAssignmentListItem, error)
	// ListAdminRoles returns the tenant's roles and the grants each confers,
	// plus true when the bounded grants read hit its cap (some roles may show an
	// incomplete grant set).
	ListAdminRoles(ctx context.Context, tenantID string) ([]RoleListItem, bool, error)
	// ListAdminIdPProviders returns the tenant's configured identity providers.
	ListAdminIdPProviders(ctx context.Context, tenantID string) ([]IdPProviderListItem, error)
	// ListAdminIdPGroupMappings returns the tenant/workspace group->role mappings.
	ListAdminIdPGroupMappings(ctx context.Context, tenantID, workspaceID string) ([]IdPGroupMappingListItem, error)
	// ListAdminAPITokens returns every user's generated tokens in the tenant/workspace.
	ListAdminAPITokens(ctx context.Context, tenantID, workspaceID string) ([]APITokenListItem, error)
}

// GovernanceAuditReader is the read surface for tenant-admin audit links.
// It wraps the governance audit store's authorized List and aggregate Summary.
type GovernanceAuditReader interface {
	// ListAuditEvents returns audit-safe events matching the bounded query.
	ListAuditEvents(ctx context.Context, query AuditQuery) ([]governanceaudit.Event, error)
	// SummarizeAuditEvents returns aggregate-only audit counts for all tenants.
	// Called by the shared operator; not called for tenant-scoped reads.
	SummarizeAuditEvents(ctx context.Context) (governanceaudit.Summary, error)
	// SummarizeAuditEventsForTenant returns aggregate-only audit counts scoped
	// to a single tenant. Global/NULL-tenant events are excluded. Called by the
	// tenant-admin summary path.
	SummarizeAuditEventsForTenant(ctx context.Context, tenantID string) (governanceaudit.Summary, error)
}

// InvitationRevokeRequest revokes one invitation in the caller's
// tenant/workspace. TenantID/WorkspaceID are taken strictly from AuthContext by
// the handler; a request body never selects the tenant.
type InvitationRevokeRequest struct {
	InviteID    string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// InvitationRevokeResult reports the terminal state of a revoke attempt.
// Found is false when no invitation with the given id exists in the
// tenant/workspace (the handler maps that to 404). Status is the row's status
// after the idempotent revoke (an already-revoked, accepted, or expired
// invitation is left unchanged and its existing status is reported).
type InvitationRevokeResult struct {
	Found   bool
	Revoked bool
	Status  string
}

// RoleAssignmentGrantRequest grants (idempotently activates) a
// membership-role row for a user in the caller's tenant/workspace. TenantID is
// taken strictly from AuthContext; WorkspaceID defaults to the AuthContext
// workspace and may be narrowed by the optional request workspace_id.
type RoleAssignmentGrantRequest struct {
	TenantID           string
	WorkspaceID        string
	UserID             string
	RoleID             string
	AssignmentSource   string
	PolicyRevisionHash string
	EffectiveAt        time.Time
}

// RoleAssignmentRevokeRequest tombstones a membership-role row for a user
// in the caller's tenant/workspace. Idempotent: an already-revoked assignment is
// a safe no-op.
type RoleAssignmentRevokeRequest struct {
	TenantID    string
	WorkspaceID string
	UserID      string
	RoleID      string
	RevokedAt   time.Time
}

// RoleAssignmentMutationResult reports whether an active assignment row was
// affected. RoleValid is false when the role does not exist (or is not active)
// in the tenant. UserValid is false when the user has no active tenant
// membership; identity_membership_roles FKs to identity_tenant_memberships so a
// grant to a non-member would fail at the DB otherwise. Both false cases map to
// 4xx. Changed reflects whether a fresh row was inserted (not reactivation); on
// reactivation Changed is false and Status carries the effective state.
type RoleAssignmentMutationResult struct {
	RoleValid bool
	UserValid bool
	Changed   bool
	Status    string
}

// IdPGroupMappingCreateRequest creates (idempotently activates) an external
// group->role mapping. ExternalGroupHash is the server-side hash of the raw
// external group name computed by the handler with the SAME hash the OIDC login
// path uses to read mappings; the raw group name never reaches the store.
type IdPGroupMappingCreateRequest struct {
	ProviderConfigID   string
	ExternalGroupHash  string
	TenantID           string
	WorkspaceID        string
	RoleID             string
	MappingSource      string
	PolicyRevisionHash string
	EffectiveAt        time.Time
}

// IdPGroupMappingDeleteRequest tombstones one external group->role mapping
// identified by its opaque MappingRef (an md5 digest over the composite key).
// The store resolves the ref tenant-scoped; the raw group name is never needed.
type IdPGroupMappingDeleteRequest struct {
	MappingRef  string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// IdPGroupMappingCreateResult reports the outcome of a mapping create.
// ProviderValid/RoleValid are false when the provider config or role does not
// exist (or is not active) in the tenant. MappingRef is the opaque reference for
// the created/activated row (same md5 form the read path returns).
type IdPGroupMappingCreateResult struct {
	ProviderValid bool
	RoleValid     bool
	Created       bool
	MappingRef    string
	Status        string
}

// IdPGroupMappingDeleteResult reports whether a mapping was tombstoned.
// Found is false when no active mapping matches the ref in the tenant/workspace
// (the handler maps that to an idempotent 200 no-op, not a fabricated row).
type IdPGroupMappingDeleteResult struct {
	Found   bool
	Deleted bool
}

// MutationStore is the write surface the admin console backend uses
// for tenant-scoped identity mutations. Every method is scoped strictly to the
// caller's tenant (and workspace where applicable); none accepts or returns a
// secret, invite code, or raw external group name. Writes are idempotent under
// retry via active-row conflict keys, not table locks.
type MutationStore interface {
	// RevokeAdminInvitation soft-revokes an invitation. It is idempotent: an
	// invitation that is already revoked/accepted/expired is left unchanged and
	// its current status is reported.
	RevokeAdminInvitation(ctx context.Context, req InvitationRevokeRequest) (InvitationRevokeResult, error)
	// GrantAdminRoleAssignment idempotently activates a membership-role row.
	// It validates the role exists and is active in the tenant before granting.
	GrantAdminRoleAssignment(ctx context.Context, req RoleAssignmentGrantRequest) (RoleAssignmentMutationResult, error)
	// RevokeAdminRoleAssignment tombstones a membership-role row. Idempotent.
	RevokeAdminRoleAssignment(ctx context.Context, req RoleAssignmentRevokeRequest) (RoleAssignmentMutationResult, error)
	// CreateAdminIdPGroupMapping idempotently activates a group->role mapping.
	// It validates the provider config and role exist and are active in the
	// tenant before creating, and returns the opaque mapping_ref.
	CreateAdminIdPGroupMapping(ctx context.Context, req IdPGroupMappingCreateRequest) (IdPGroupMappingCreateResult, error)
	// DeleteAdminIdPGroupMapping tombstones one mapping resolved by its opaque
	// mapping_ref, tenant-scoped. Idempotent.
	DeleteAdminIdPGroupMapping(ctx context.Context, req IdPGroupMappingDeleteRequest) (IdPGroupMappingDeleteResult, error)
}
