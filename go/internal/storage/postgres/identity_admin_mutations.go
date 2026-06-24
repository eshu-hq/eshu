package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// AdminInvitationRevoke soft-revokes one invitation in the caller's
// tenant/workspace. The store never receives or returns an invite code.
type AdminInvitationRevoke struct {
	InviteID    string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// AdminInvitationRevokeResult reports the terminal state of a revoke. Found is
// false when no live invitation with the id exists in the tenant/workspace.
// Revoked is true only when this call transitioned an active invitation to
// revoked; an already-terminal invitation reports Revoked=false with its current
// status (idempotent no-op).
type AdminInvitationRevokeResult struct {
	Found   bool
	Revoked bool
	Status  string
}

// AdminRoleAssignmentGrant idempotently activates a membership-role row.
type AdminRoleAssignmentGrant struct {
	TenantID           string
	WorkspaceID        string
	UserID             string
	RoleID             string
	AssignmentSource   string
	PolicyRevisionHash string
	EffectiveAt        time.Time
}

// AdminRoleAssignmentRevoke tombstones a membership-role row.
type AdminRoleAssignmentRevoke struct {
	TenantID    string
	WorkspaceID string
	UserID      string
	RoleID      string
	RevokedAt   time.Time
}

// AdminRoleAssignmentResult reports the outcome of a grant or revoke. RoleValid
// is false when the role does not exist or is not active in the tenant.
// UserValid is false when the user has no active tenant membership and the grant
// cannot be written (FK constraint on identity_tenant_memberships). Changed is
// true when this call altered an active-row count (a fresh grant or an actual
// revoke); a repeated grant/revoke reports Changed=false.
type AdminRoleAssignmentResult struct {
	RoleValid bool
	UserValid bool
	Changed   bool
	Status    string
}

// AdminIdPGroupMappingCreate idempotently activates a group->role mapping. The
// store receives only ExternalGroupHash (the precomputed hash); the raw external
// group name never reaches this layer.
type AdminIdPGroupMappingCreate struct {
	ProviderConfigID   string
	ExternalGroupHash  string
	TenantID           string
	WorkspaceID        string
	RoleID             string
	MappingSource      string
	PolicyRevisionHash string
	EffectiveAt        time.Time
}

// AdminIdPGroupMappingDelete tombstones one mapping resolved by its opaque
// mapping_ref, tenant/workspace scoped.
type AdminIdPGroupMappingDelete struct {
	MappingRef  string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// AdminIdPGroupMappingCreateResult reports the outcome of a mapping create.
// ProviderValid/RoleValid are false when the provider config or role does not
// exist or is not active in the tenant. MappingRef is the opaque reference for
// the activated row (same md5 form the read path returns).
type AdminIdPGroupMappingCreateResult struct {
	ProviderValid bool
	RoleValid     bool
	Created       bool
	MappingRef    string
	Status        string
}

// AdminIdPGroupMappingDeleteResult reports whether a mapping was tombstoned.
type AdminIdPGroupMappingDeleteResult struct {
	Found   bool
	Deleted bool
}

// RevokeAdminInvitation soft-revokes one invitation scoped strictly to the
// supplied tenant and workspace. It is idempotent: an invitation that is already
// revoked, accepted, or expired is left unchanged and its current status is
// reported. A missing invitation returns Found=false (not an error). The
// status read and revoke run in one transaction with a row lock so a concurrent
// revoke cannot interleave.
func (s *IdentitySubjectStore) RevokeAdminInvitation(
	ctx context.Context,
	revoke AdminInvitationRevoke,
) (AdminInvitationRevokeResult, error) {
	if s.db == nil {
		return AdminInvitationRevokeResult{}, errors.New("identity subject store database is required")
	}
	revoke.InviteID = strings.TrimSpace(revoke.InviteID)
	revoke.TenantID = strings.TrimSpace(revoke.TenantID)
	revoke.WorkspaceID = strings.TrimSpace(revoke.WorkspaceID)
	if revoke.InviteID == "" || revoke.TenantID == "" {
		return AdminInvitationRevokeResult{}, errors.New("invite_id and tenant_id are required")
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return AdminInvitationRevokeResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var status string
	var revokedAt, acceptedAt sql.NullTime
	var expiresAt time.Time
	statusRows, err := tx.QueryContext(ctx, selectAdminInvitationStatusQuery, revoke.InviteID, revoke.TenantID, revoke.WorkspaceID)
	if err != nil {
		return AdminInvitationRevokeResult{}, fmt.Errorf("select admin invitation status: %w", err)
	}
	found, err := scanSingleInvitationStatus(statusRows, &status, &revokedAt, &acceptedAt, &expiresAt)
	if err != nil {
		return AdminInvitationRevokeResult{}, fmt.Errorf("select admin invitation status: %w", err)
	}
	if !found {
		if err := tx.Commit(); err != nil {
			return AdminInvitationRevokeResult{}, fmt.Errorf("commit admin invitation revoke: %w", err)
		}
		committed = true
		return AdminInvitationRevokeResult{Found: false}, nil
	}

	// Already terminal (revoked, accepted, expired, or any non-active status):
	// idempotent no-op. Report the existing status without writing.
	if status != "active" || revokedAt.Valid || acceptedAt.Valid {
		if err := tx.Commit(); err != nil {
			return AdminInvitationRevokeResult{}, fmt.Errorf("commit admin invitation revoke: %w", err)
		}
		committed = true
		return AdminInvitationRevokeResult{Found: true, Revoked: false, Status: status}, nil
	}

	result, err := tx.ExecContext(ctx, revokeAdminInvitationQuery, revoke.InviteID, revoke.TenantID, revoke.WorkspaceID, revoke.RevokedAt.UTC())
	if err != nil {
		return AdminInvitationRevokeResult{}, fmt.Errorf("revoke admin invitation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return AdminInvitationRevokeResult{}, fmt.Errorf("revoke admin invitation rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AdminInvitationRevokeResult{}, fmt.Errorf("commit admin invitation revoke: %w", err)
	}
	committed = true
	// affected==1 in the normal path; a concurrent revoke that won the row lock
	// first leaves affected==0, which is still a correct idempotent outcome.
	return AdminInvitationRevokeResult{Found: true, Revoked: affected == 1, Status: "revoked"}, nil
}

// GrantAdminRoleAssignment idempotently activates a membership-role row scoped
// strictly to the supplied tenant and workspace. It validates the role exists
// and is active in the tenant, and that the user has an active tenant
// membership, before granting. An unknown/tombstoned role returns
// RoleValid=false; a user with no active membership returns UserValid=false;
// both return without writing. A concurrent double grant converges on one row
// via the primary-key conflict target.
func (s *IdentitySubjectStore) GrantAdminRoleAssignment(
	ctx context.Context,
	grant AdminRoleAssignmentGrant,
) (AdminRoleAssignmentResult, error) {
	if s.db == nil {
		return AdminRoleAssignmentResult{}, errors.New("identity subject store database is required")
	}
	grant.TenantID = strings.TrimSpace(grant.TenantID)
	grant.WorkspaceID = strings.TrimSpace(grant.WorkspaceID)
	grant.UserID = strings.TrimSpace(grant.UserID)
	grant.RoleID = strings.TrimSpace(grant.RoleID)
	if grant.TenantID == "" || grant.UserID == "" || grant.RoleID == "" {
		return AdminRoleAssignmentResult{}, errors.New("tenant_id, user_id, and role_id are required")
	}
	if grant.AssignmentSource == "" {
		grant.AssignmentSource = "admin"
	}

	roleActive, err := s.activeRoleExists(ctx, grant.TenantID, grant.RoleID)
	if err != nil {
		return AdminRoleAssignmentResult{}, err
	}
	if !roleActive {
		return AdminRoleAssignmentResult{RoleValid: false}, nil
	}

	// Membership precheck: identity_membership_roles has a FK to
	// identity_tenant_memberships(tenant_id, workspace_id, user_id). Granting a
	// role to a user with no membership row would FK-fail (23503) → 500. A
	// precheck converts that to a meaningful UserValid=false → 400 caller error
	// without writing. Belt-and-suspenders: a 23503 from the INSERT (race between
	// precheck and membership deletion) is also classified as UserValid=false.
	memberActive, err := s.activeMembershipExists(ctx, grant.TenantID, grant.WorkspaceID, grant.UserID)
	if err != nil {
		return AdminRoleAssignmentResult{}, err
	}
	if !memberActive {
		return AdminRoleAssignmentResult{RoleValid: true, UserValid: false}, nil
	}

	var status string
	var inserted bool
	grantRows, err := s.db.QueryContext(
		ctx,
		grantAdminRoleAssignmentQuery,
		grant.TenantID,
		grant.WorkspaceID,
		grant.UserID,
		grant.RoleID,
		grant.AssignmentSource,
		grant.PolicyRevisionHash,
		grant.EffectiveAt.UTC(),
	)
	if err != nil {
		// Belt-and-suspenders: classify FK violation (23503) as UserValid=false so
		// a membership deleted between precheck and INSERT still surfaces as a 400
		// rather than a 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return AdminRoleAssignmentResult{RoleValid: true, UserValid: false}, nil
		}
		return AdminRoleAssignmentResult{}, fmt.Errorf("grant admin role assignment: %w", err)
	}
	if err := scanStatusInserted(grantRows, &status, &inserted); err != nil {
		return AdminRoleAssignmentResult{}, fmt.Errorf("grant admin role assignment: %w", err)
	}
	return AdminRoleAssignmentResult{RoleValid: true, UserValid: true, Changed: inserted, Status: status}, nil
}

// RevokeAdminRoleAssignment tombstones one active membership-role row scoped
// strictly to the supplied tenant and workspace. Idempotent: an already-revoked
// or absent assignment is a no-op reporting Changed=false.
func (s *IdentitySubjectStore) RevokeAdminRoleAssignment(
	ctx context.Context,
	revoke AdminRoleAssignmentRevoke,
) (AdminRoleAssignmentResult, error) {
	if s.db == nil {
		return AdminRoleAssignmentResult{}, errors.New("identity subject store database is required")
	}
	revoke.TenantID = strings.TrimSpace(revoke.TenantID)
	revoke.WorkspaceID = strings.TrimSpace(revoke.WorkspaceID)
	revoke.UserID = strings.TrimSpace(revoke.UserID)
	revoke.RoleID = strings.TrimSpace(revoke.RoleID)
	if revoke.TenantID == "" || revoke.UserID == "" || revoke.RoleID == "" {
		return AdminRoleAssignmentResult{}, errors.New("tenant_id, user_id, and role_id are required")
	}
	result, err := s.db.ExecContext(
		ctx,
		revokeAdminRoleAssignmentQuery,
		revoke.TenantID,
		revoke.WorkspaceID,
		revoke.UserID,
		revoke.RoleID,
		revoke.RevokedAt.UTC(),
	)
	if err != nil {
		return AdminRoleAssignmentResult{}, fmt.Errorf("revoke admin role assignment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return AdminRoleAssignmentResult{}, fmt.Errorf("revoke admin role assignment rows affected: %w", err)
	}
	// RoleValid is true here: revoking an assignment never needs the role to
	// still be active, and an absent assignment is a safe no-op.
	return AdminRoleAssignmentResult{RoleValid: true, Changed: affected == 1, Status: "revoked"}, nil
}

// CreateAdminIdPGroupMapping idempotently activates a group->role mapping scoped
// strictly to the supplied tenant and workspace. It validates the provider
// config and role exist and are active in the tenant before creating; an unknown
// or tombstoned provider/role returns ProviderValid/RoleValid=false without
// writing. The external group is supplied only as its hash. A concurrent
// re-create converges on one row via the primary-key conflict target.
func (s *IdentitySubjectStore) CreateAdminIdPGroupMapping(
	ctx context.Context,
	create AdminIdPGroupMappingCreate,
) (AdminIdPGroupMappingCreateResult, error) {
	if s.db == nil {
		return AdminIdPGroupMappingCreateResult{}, errors.New("identity subject store database is required")
	}
	create.ProviderConfigID = strings.TrimSpace(create.ProviderConfigID)
	create.ExternalGroupHash = strings.TrimSpace(create.ExternalGroupHash)
	create.TenantID = strings.TrimSpace(create.TenantID)
	create.WorkspaceID = strings.TrimSpace(create.WorkspaceID)
	create.RoleID = strings.TrimSpace(create.RoleID)
	if create.ProviderConfigID == "" || create.ExternalGroupHash == "" || create.TenantID == "" || create.RoleID == "" {
		return AdminIdPGroupMappingCreateResult{}, errors.New("provider_config_id, external_group_hash, tenant_id, and role_id are required")
	}
	if create.MappingSource == "" {
		create.MappingSource = "admin"
	}

	providerActive, err := s.activeProviderExists(ctx, create.ProviderConfigID, create.TenantID)
	if err != nil {
		return AdminIdPGroupMappingCreateResult{}, err
	}
	if !providerActive {
		return AdminIdPGroupMappingCreateResult{ProviderValid: false}, nil
	}
	roleActive, err := s.activeRoleExists(ctx, create.TenantID, create.RoleID)
	if err != nil {
		return AdminIdPGroupMappingCreateResult{}, err
	}
	if !roleActive {
		return AdminIdPGroupMappingCreateResult{ProviderValid: true, RoleValid: false}, nil
	}

	var mappingRef, status string
	var inserted bool
	mappingRows, err := s.db.QueryContext(
		ctx,
		createAdminIdPGroupMappingQuery,
		create.ProviderConfigID,
		create.ExternalGroupHash,
		create.TenantID,
		create.WorkspaceID,
		create.RoleID,
		create.MappingSource,
		create.PolicyRevisionHash,
		create.EffectiveAt.UTC(),
	)
	if err != nil {
		return AdminIdPGroupMappingCreateResult{}, fmt.Errorf("create admin idp group mapping: %w", err)
	}
	if err := scanMappingRefStatusInserted(mappingRows, &mappingRef, &status, &inserted); err != nil {
		return AdminIdPGroupMappingCreateResult{}, fmt.Errorf("create admin idp group mapping: %w", err)
	}
	return AdminIdPGroupMappingCreateResult{
		ProviderValid: true,
		RoleValid:     true,
		Created:       inserted,
		MappingRef:    mappingRef,
		Status:        status,
	}, nil
}

// DeleteAdminIdPGroupMapping tombstones one active group->role mapping resolved
// by its opaque mapping_ref, scoped strictly to the supplied tenant and
// workspace. Idempotent: an already-deleted or absent mapping reports
// Found=false without error.
func (s *IdentitySubjectStore) DeleteAdminIdPGroupMapping(
	ctx context.Context,
	del AdminIdPGroupMappingDelete,
) (AdminIdPGroupMappingDeleteResult, error) {
	if s.db == nil {
		return AdminIdPGroupMappingDeleteResult{}, errors.New("identity subject store database is required")
	}
	del.MappingRef = strings.TrimSpace(del.MappingRef)
	del.TenantID = strings.TrimSpace(del.TenantID)
	del.WorkspaceID = strings.TrimSpace(del.WorkspaceID)
	if del.MappingRef == "" || del.TenantID == "" {
		return AdminIdPGroupMappingDeleteResult{}, errors.New("mapping_ref and tenant_id are required")
	}
	rows, err := s.db.QueryContext(
		ctx,
		deleteAdminIdPGroupMappingQuery,
		del.TenantID,
		del.WorkspaceID,
		del.RevokedAt.UTC(),
		del.MappingRef,
	)
	if err != nil {
		return AdminIdPGroupMappingDeleteResult{}, fmt.Errorf("delete admin idp group mapping: %w", err)
	}
	defer func() { _ = rows.Close() }()
	deleted := false
	for rows.Next() {
		var providerConfigID string
		if err := rows.Scan(&providerConfigID); err != nil {
			return AdminIdPGroupMappingDeleteResult{}, fmt.Errorf("scan admin idp group mapping delete: %w", err)
		}
		deleted = true
	}
	if err := rows.Err(); err != nil {
		return AdminIdPGroupMappingDeleteResult{}, fmt.Errorf("delete admin idp group mapping: %w", err)
	}
	return AdminIdPGroupMappingDeleteResult{Found: deleted, Deleted: deleted}, nil
}

// activeMembershipExists reports whether a user has an active, non-disabled,
// non-tombstoned tenant/workspace membership. identity_membership_roles has a
// FK to identity_tenant_memberships(tenant_id,workspace_id,user_id); this
// precheck converts a foreseeable FK violation into a UserValid=false 4xx
// instead of a server error.
func (s *IdentitySubjectStore) activeMembershipExists(ctx context.Context, tenantID, workspaceID, userID string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, selectActiveMembershipExistsQuery, tenantID, workspaceID, userID)
	if err != nil {
		return false, fmt.Errorf("select active membership: %w", err)
	}
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("select active membership: %w", err)
	}
	return exists, nil
}

// activeRoleExists reports whether a role is active and not tombstoned in the
// tenant.
func (s *IdentitySubjectStore) activeRoleExists(ctx context.Context, tenantID, roleID string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, selectActiveRoleExistsQuery, tenantID, roleID)
	if err != nil {
		return false, fmt.Errorf("select active role: %w", err)
	}
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("select active role: %w", err)
	}
	return exists, nil
}

// activeProviderExists reports whether a provider config is active and not
// tombstoned in the tenant.
func (s *IdentitySubjectStore) activeProviderExists(ctx context.Context, providerConfigID, tenantID string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, selectActiveProviderExistsQuery, providerConfigID, tenantID)
	if err != nil {
		return false, fmt.Errorf("select active provider: %w", err)
	}
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("select active provider: %w", err)
	}
	return exists, nil
}

// scanSingleInvitationStatus scans the optional single invitation-status row.
func scanSingleInvitationStatus(rows Rows, status *string, revokedAt, acceptedAt *sql.NullTime, expiresAt *time.Time) (bool, error) {
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	if err := rows.Scan(status, revokedAt, acceptedAt, expiresAt); err != nil {
		return false, err
	}
	return true, rows.Err()
}

// scanStatusInserted scans a single (status, inserted) row from a RETURNING upsert.
func scanStatusInserted(rows Rows, status *string, inserted *bool) error {
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errors.New("upsert returned no row")
	}
	if err := rows.Scan(status, inserted); err != nil {
		return err
	}
	return rows.Err()
}

// scanMappingRefStatusInserted scans a single (mapping_ref, status, inserted) row.
func scanMappingRefStatusInserted(rows Rows, mappingRef, status *string, inserted *bool) error {
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errors.New("upsert returned no row")
	}
	if err := rows.Scan(mappingRef, status, inserted); err != nil {
		return err
	}
	return rows.Err()
}
