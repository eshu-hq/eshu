package query

import (
	"context"
	"time"
)

// AdminInvitationRevokeRequest revokes one invitation in the caller's
// tenant/workspace. TenantID/WorkspaceID are taken strictly from AuthContext by
// the handler; a request body never selects the tenant.
type AdminInvitationRevokeRequest struct {
	InviteID    string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// AdminInvitationRevokeResult reports the terminal state of a revoke attempt.
// Found is false when no invitation with the given id exists in the
// tenant/workspace (the handler maps that to 404). Status is the row's status
// after the idempotent revoke (an already-revoked, accepted, or expired
// invitation is left unchanged and its existing status is reported).
type AdminInvitationRevokeResult struct {
	Found   bool
	Revoked bool
	Status  string
}

// AdminRoleAssignmentGrantRequest grants (idempotently activates) a
// membership-role row for a user in the caller's tenant/workspace. TenantID is
// taken strictly from AuthContext; WorkspaceID defaults to the AuthContext
// workspace and may be narrowed by the optional request workspace_id.
type AdminRoleAssignmentGrantRequest struct {
	TenantID           string
	WorkspaceID        string
	UserID             string
	RoleID             string
	AssignmentSource   string
	PolicyRevisionHash string
	EffectiveAt        time.Time
}

// AdminRoleAssignmentRevokeRequest tombstones a membership-role row for a user
// in the caller's tenant/workspace. Idempotent: an already-revoked assignment is
// a safe no-op.
type AdminRoleAssignmentRevokeRequest struct {
	TenantID    string
	WorkspaceID string
	UserID      string
	RoleID      string
	RevokedAt   time.Time
}

// AdminRoleAssignmentMutationResult reports whether an active assignment row was
// affected. RoleValid is false when the role does not exist (or is not active)
// in the tenant, which the handler maps to a 4xx rather than fabricating a row.
type AdminRoleAssignmentMutationResult struct {
	RoleValid bool
	Changed   bool
	Status    string
}

// AdminIdPGroupMappingCreateRequest creates (idempotently activates) an external
// group->role mapping. ExternalGroupHash is the server-side hash of the raw
// external group name computed by the handler with the SAME hash the OIDC login
// path uses to read mappings; the raw group name never reaches the store.
type AdminIdPGroupMappingCreateRequest struct {
	ProviderConfigID   string
	ExternalGroupHash  string
	TenantID           string
	WorkspaceID        string
	RoleID             string
	MappingSource      string
	PolicyRevisionHash string
	EffectiveAt        time.Time
}

// AdminIdPGroupMappingDeleteRequest tombstones one external group->role mapping
// identified by its opaque MappingRef (an md5 digest over the composite key).
// The store resolves the ref tenant-scoped; the raw group name is never needed.
type AdminIdPGroupMappingDeleteRequest struct {
	MappingRef  string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// AdminIdPGroupMappingCreateResult reports the outcome of a mapping create.
// ProviderValid/RoleValid are false when the provider config or role does not
// exist (or is not active) in the tenant. MappingRef is the opaque reference for
// the created/activated row (same md5 form the read path returns).
type AdminIdPGroupMappingCreateResult struct {
	ProviderValid bool
	RoleValid     bool
	Created       bool
	MappingRef    string
	Status        string
}

// AdminIdPGroupMappingDeleteResult reports whether a mapping was tombstoned.
// Found is false when no active mapping matches the ref in the tenant/workspace
// (the handler maps that to an idempotent 200 no-op, not a fabricated row).
type AdminIdPGroupMappingDeleteResult struct {
	Found   bool
	Deleted bool
}

// AdminIdentityMutationStore is the write surface the admin console backend uses
// for tenant-scoped identity mutations. Every method is scoped strictly to the
// caller's tenant (and workspace where applicable); none accepts or returns a
// secret, invite code, or raw external group name. Writes are idempotent under
// retry via active-row conflict keys, not table locks.
type AdminIdentityMutationStore interface {
	// RevokeAdminInvitation soft-revokes an invitation. It is idempotent: an
	// invitation that is already revoked/accepted/expired is left unchanged and
	// its current status is reported.
	RevokeAdminInvitation(ctx context.Context, req AdminInvitationRevokeRequest) (AdminInvitationRevokeResult, error)
	// GrantAdminRoleAssignment idempotently activates a membership-role row.
	// It validates the role exists and is active in the tenant before granting.
	GrantAdminRoleAssignment(ctx context.Context, req AdminRoleAssignmentGrantRequest) (AdminRoleAssignmentMutationResult, error)
	// RevokeAdminRoleAssignment tombstones a membership-role row. Idempotent.
	RevokeAdminRoleAssignment(ctx context.Context, req AdminRoleAssignmentRevokeRequest) (AdminRoleAssignmentMutationResult, error)
	// CreateAdminIdPGroupMapping idempotently activates a group->role mapping.
	// It validates the provider config and role exist and are active in the
	// tenant before creating, and returns the opaque mapping_ref.
	CreateAdminIdPGroupMapping(ctx context.Context, req AdminIdPGroupMappingCreateRequest) (AdminIdPGroupMappingCreateResult, error)
	// DeleteAdminIdPGroupMapping tombstones one mapping resolved by its opaque
	// mapping_ref, tenant-scoped. Idempotent.
	DeleteAdminIdPGroupMapping(ctx context.Context, req AdminIdPGroupMappingDeleteRequest) (AdminIdPGroupMappingDeleteResult, error)
}
