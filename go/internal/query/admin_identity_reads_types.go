// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// AdminInvitationListItem is the metadata-only invitation view returned to a
// tenant admin. It never carries an invite code, invitee handle, or inviter
// identity (those are stored only as hashes and are never read back).
type AdminInvitationListItem struct {
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

// AdminRoleAssignmentListItem is the metadata-only membership-role assignment
// view returned to a tenant admin.
type AdminRoleAssignmentListItem struct {
	UserID           string
	RoleID           string
	AssignmentSource string
	Status           string
	EffectiveAt      time.Time
	ExpiresAt        time.Time
	TenantID         string
	WorkspaceID      string
}

// AdminRoleGrantListItem is one capability grant attached to a role.
type AdminRoleGrantListItem struct {
	GrantID    string
	Action     string
	Feature    string
	DataClass  string
	ScopeClass string
	Status     string
}

// AdminRoleListItem is a tenant role plus the grants it confers, returned to a
// tenant admin so they can see what each role permits.
type AdminRoleListItem struct {
	RoleID  string
	Status  string
	BuiltIn bool
	Grants  []AdminRoleGrantListItem
}

// AdminIdPProviderListItem is the metadata-only identity-provider view returned
// to a tenant admin. It never carries issuer/metadata/entity/client hashes or
// credential handles.
type AdminIdPProviderListItem struct {
	ProviderConfigID string
	ProviderKind     string
	Status           string
}

// AdminIdPGroupMappingListItem is the metadata-only group->role mapping view
// returned to a tenant admin. MappingRef is an opaque, non-secret reference; the
// hashed external group name is never returned.
type AdminIdPGroupMappingListItem struct {
	MappingRef       string
	ProviderConfigID string
	RoleID           string
	Status           string
	EffectiveAt      time.Time
	ExpiresAt        time.Time
	TenantID         string
	WorkspaceID      string
}

// AdminAPITokenListItem is the metadata-only generated-token view returned to a
// tenant admin across all users. It never carries token_hash or display label
// hashes.
type AdminAPITokenListItem struct {
	TokenID            string
	TokenClass         string
	UserID             string
	ServicePrincipalID string
	Status             string
	IssuedAt           time.Time
	ExpiresAt          time.Time
	RevokedAt          time.Time
	TenantID           string
	WorkspaceID        string
}

// AdminAuditQuery bounds an admin audit-event read. OperatorAuthorized is set by
// the handler only after the shared-operator gate passes; the underlying store
// refuses to return detailed events when it is false.
type AdminAuditQuery struct {
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
}

// AdminIdentityReadStore is the read surface the admin console backend uses for
// tenant-scoped identity metadata. Every method is scoped strictly to the
// caller's tenant (and workspace where applicable); none returns a secret,
// hash, invite code, credential handle, or external group hash.
type AdminIdentityReadStore interface {
	// ListAdminInvitations returns invitations in the tenant/workspace.
	ListAdminInvitations(ctx context.Context, tenantID, workspaceID string) ([]AdminInvitationListItem, error)
	// ListAdminRoleAssignments returns membership-role assignments in the
	// tenant/workspace, optionally filtered by userID (blank lists all).
	ListAdminRoleAssignments(ctx context.Context, tenantID, workspaceID, userID string) ([]AdminRoleAssignmentListItem, error)
	// ListAdminRoles returns the tenant's roles and the grants each confers,
	// plus true when the bounded grants read hit its cap (some roles may show an
	// incomplete grant set).
	ListAdminRoles(ctx context.Context, tenantID string) ([]AdminRoleListItem, bool, error)
	// ListAdminIdPProviders returns the tenant's configured identity providers.
	ListAdminIdPProviders(ctx context.Context, tenantID string) ([]AdminIdPProviderListItem, error)
	// ListAdminIdPGroupMappings returns the tenant/workspace group->role mappings.
	ListAdminIdPGroupMappings(ctx context.Context, tenantID, workspaceID string) ([]AdminIdPGroupMappingListItem, error)
	// ListAdminAPITokens returns every user's generated tokens in the tenant/workspace.
	ListAdminAPITokens(ctx context.Context, tenantID, workspaceID string) ([]AdminAPITokenListItem, error)
}

// AdminGovernanceAuditReader is the read surface for tenant-admin audit links.
// It wraps the governance audit store's authorized List and aggregate Summary.
type AdminGovernanceAuditReader interface {
	// ListAuditEvents returns audit-safe events matching the bounded query.
	ListAuditEvents(ctx context.Context, query AdminAuditQuery) ([]governanceaudit.Event, error)
	// SummarizeAuditEvents returns aggregate-only audit counts.
	SummarizeAuditEvents(ctx context.Context) (governanceaudit.Summary, error)
}
