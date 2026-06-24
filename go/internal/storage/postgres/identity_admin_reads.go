package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AdminInvitationListItem is the metadata-only view of one identity invitation
// that is safe to return to a tenant admin. It never includes invite_code_hash,
// invitee_handle_hash, or inviter_subject_id_hash (all hashed secrets).
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

// AdminRoleAssignmentListItem is the metadata-only view of one membership-role
// assignment. Every column is safe metadata; the table holds no hashed secret
// beyond policy_revision_hash, which is never selected.
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

// AdminRoleListItem is the metadata-only view of one tenant role together with
// the grants that role confers. role_key_hash and policy_revision_hash are
// hashed and never selected.
type AdminRoleListItem struct {
	RoleID  string
	Status  string
	BuiltIn bool
	Grants  []AdminRoleGrantListItem
}

// AdminRoleGrantListItem is one capability grant attached to a role. The grant
// describes what the role permits; scope_id_hash and repository_id_hash are
// hashed scope selectors and are never selected.
type AdminRoleGrantListItem struct {
	GrantID    string
	Action     string
	Feature    string
	DataClass  string
	ScopeClass string
	Status     string
}

// listAdminInvitationsQuery selects metadata-only columns for invitations in the
// caller's tenant/workspace. It never selects invite_code_hash,
// invitee_handle_hash, or inviter_subject_id_hash. tombstoned_at IS NULL
// excludes soft-deleted rows that may still carry status='active'.
const listAdminInvitationsQuery = `
SELECT
    invite_id,
    role_id,
    status,
    expires_at,
    accepted_at,
    revoked_at,
    created_at,
    updated_at,
    tenant_id,
    workspace_id
FROM identity_invitations
WHERE tenant_id = $1 AND workspace_id = $2
  AND tombstoned_at IS NULL
ORDER BY created_at DESC, invite_id ASC
LIMIT 500
`

// ListAdminInvitations returns metadata-only invitation rows scoped strictly to
// the supplied tenant and workspace. It never returns invite codes or handle
// hashes.
func (s *IdentitySubjectStore) ListAdminInvitations(
	ctx context.Context,
	tenantID string,
	workspaceID string,
) ([]AdminInvitationListItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	rows, err := s.db.QueryContext(ctx, listAdminInvitationsQuery, tenantID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list admin invitations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminInvitationListItem
	for rows.Next() {
		var item AdminInvitationListItem
		var acceptedAt, revokedAt sql.NullTime
		if err := rows.Scan(
			&item.InviteID,
			&item.RoleID,
			&item.Status,
			&item.ExpiresAt,
			&acceptedAt,
			&revokedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.TenantID,
			&item.WorkspaceID,
		); err != nil {
			return nil, fmt.Errorf("scan admin invitation item: %w", err)
		}
		item.ExpiresAt = item.ExpiresAt.UTC()
		item.CreatedAt = item.CreatedAt.UTC()
		item.UpdatedAt = item.UpdatedAt.UTC()
		if acceptedAt.Valid {
			item.AcceptedAt = acceptedAt.Time.UTC()
		}
		if revokedAt.Valid {
			item.RevokedAt = revokedAt.Time.UTC()
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin invitations: %w", err)
	}
	return items, nil
}

// listAdminRoleAssignmentsQuery selects metadata-only membership-role rows for
// the caller's tenant/workspace, optionally filtered by user_id. When the
// user_id parameter ($3) is blank the filter is a no-op. tombstoned_at IS NULL
// excludes soft-deleted rows that may still carry status='active'.
const listAdminRoleAssignmentsQuery = `
SELECT
    user_id,
    role_id,
    assignment_source,
    status,
    effective_at,
    expires_at,
    tenant_id,
    workspace_id
FROM identity_membership_roles
WHERE tenant_id = $1
  AND workspace_id = $2
  AND ($3 = '' OR user_id = $3)
  AND tombstoned_at IS NULL
ORDER BY user_id ASC, role_id ASC
LIMIT 500
`

// ListAdminRoleAssignments returns metadata-only membership-role assignments
// scoped strictly to the supplied tenant and workspace, optionally filtered by
// userID. A blank userID lists every assignment in the workspace.
func (s *IdentitySubjectStore) ListAdminRoleAssignments(
	ctx context.Context,
	tenantID string,
	workspaceID string,
	userID string,
) ([]AdminRoleAssignmentListItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	rows, err := s.db.QueryContext(ctx, listAdminRoleAssignmentsQuery, tenantID, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("list admin role assignments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminRoleAssignmentListItem
	for rows.Next() {
		var item AdminRoleAssignmentListItem
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.UserID,
			&item.RoleID,
			&item.AssignmentSource,
			&item.Status,
			&item.EffectiveAt,
			&expiresAt,
			&item.TenantID,
			&item.WorkspaceID,
		); err != nil {
			return nil, fmt.Errorf("scan admin role assignment item: %w", err)
		}
		item.EffectiveAt = item.EffectiveAt.UTC()
		if expiresAt.Valid {
			item.ExpiresAt = expiresAt.Time.UTC()
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin role assignments: %w", err)
	}
	return items, nil
}

// listAdminRolesQuery selects metadata-only role rows for the caller's tenant.
// role_key_hash and policy_revision_hash are never selected. tombstoned_at IS
// NULL excludes soft-deleted rows that may still carry status='active'.
const listAdminRolesQuery = `
SELECT
    role_id,
    status,
    built_in
FROM identity_roles
WHERE tenant_id = $1
  AND tombstoned_at IS NULL
ORDER BY role_id ASC
LIMIT 500
`

// listAdminRoleGrantsQuery selects metadata-only grant rows for the caller's
// tenant. scope_id_hash, repository_id_hash, and policy_revision_hash are never
// selected. tombstoned_at IS NULL excludes soft-deleted grants attached to
// still-active roles; both the role set and grant set must agree on liveness.
const listAdminRoleGrantsQuery = `
SELECT
    role_id,
    grant_id,
    action,
    feature,
    data_class,
    scope_class,
    status
FROM identity_role_grants
WHERE tenant_id = $1
  AND tombstoned_at IS NULL
ORDER BY role_id ASC, grant_id ASC
LIMIT 500
`

// ListAdminRoles returns metadata-only role rows for the supplied tenant, each
// joined with the grants that role confers. Roles and grants are read in two
// bounded queries and stitched in memory so an admin sees what each role grants
// without exposing hashed scope selectors.
// ListAdminRoles returns the tenant's roles with their grants, plus a
// grantsTruncated flag that is true when the bounded grants query hit its cap
// (so some roles may show an incomplete grant set). The handler ORs this into
// the response "truncated" signal.
func (s *IdentitySubjectStore) ListAdminRoles(
	ctx context.Context,
	tenantID string,
) ([]AdminRoleListItem, bool, error) {
	if s.db == nil {
		return nil, false, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, false, errors.New("tenant_id is required")
	}
	roles, order, err := s.scanAdminRoles(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	grantsTruncated, err := s.attachAdminRoleGrants(ctx, tenantID, roles)
	if err != nil {
		return nil, false, err
	}
	items := make([]AdminRoleListItem, 0, len(order))
	for _, roleID := range order {
		items = append(items, *roles[roleID])
	}
	return items, grantsTruncated, nil
}

// scanAdminRoles reads the tenant's roles and returns them keyed by role_id plus
// a stable ordering slice for deterministic output.
func (s *IdentitySubjectStore) scanAdminRoles(
	ctx context.Context,
	tenantID string,
) (map[string]*AdminRoleListItem, []string, error) {
	rows, err := s.db.QueryContext(ctx, listAdminRolesQuery, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("list admin roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	roles := make(map[string]*AdminRoleListItem)
	var order []string
	for rows.Next() {
		var item AdminRoleListItem
		if err := rows.Scan(&item.RoleID, &item.Status, &item.BuiltIn); err != nil {
			return nil, nil, fmt.Errorf("scan admin role item: %w", err)
		}
		role := item
		roles[role.RoleID] = &role
		order = append(order, role.RoleID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list admin roles: %w", err)
	}
	return roles, order, nil
}

// attachAdminRoleGrants reads the tenant's role grants and appends each grant to
// the role it belongs to. Grants for unknown roles (none, given the foreign key)
// are ignored. It returns true when the bounded grants query (LIMIT 500 in
// listAdminRoleGrantsQuery) returned exactly its cap — meaning grants past the
// cap were dropped tenant-wide and some roles may show an incomplete grant set.
func (s *IdentitySubjectStore) attachAdminRoleGrants(
	ctx context.Context,
	tenantID string,
	roles map[string]*AdminRoleListItem,
) (bool, error) {
	rows, err := s.db.QueryContext(ctx, listAdminRoleGrantsQuery, tenantID)
	if err != nil {
		return false, fmt.Errorf("list admin role grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	grantCount := 0
	for rows.Next() {
		var roleID string
		var grant AdminRoleGrantListItem
		if err := rows.Scan(
			&roleID,
			&grant.GrantID,
			&grant.Action,
			&grant.Feature,
			&grant.DataClass,
			&grant.ScopeClass,
			&grant.Status,
		); err != nil {
			return false, fmt.Errorf("scan admin role grant item: %w", err)
		}
		grantCount++
		if role, ok := roles[roleID]; ok {
			role.Grants = append(role.Grants, grant)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("list admin role grants: %w", err)
	}
	// 500 == the LIMIT in listAdminRoleGrantsQuery.
	return grantCount == 500, nil
}
