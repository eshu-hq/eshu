package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- SQL security / idempotency / tenant-scope assertions ---

// TestAdminMutationQueriesAreTenantScopedAndIdempotent verifies every mutation
// SQL string is parameterized, tenant-scoped, idempotent (terminal-state guard
// or ON CONFLICT conflict key), and never references a secret, invite code, or
// raw external group column.
func TestAdminMutationQueriesAreTenantScopedAndIdempotent(t *testing.T) {
	t.Parallel()

	type sqlCase struct {
		name      string
		query     string
		mustHave  []string
		forbidden []string
	}
	cases := []sqlCase{
		{
			name:  "revoke invitation is active-guarded and tenant scoped",
			query: revokeAdminInvitationQuery,
			// status='active' + revoked_at IS NULL + accepted_at IS NULL makes a
			// repeated revoke a zero-row no-op; tenant_id/workspace_id scope it.
			mustHave:  []string{"UPDATE identity_invitations", "tenant_id = $2", "workspace_id = $3", "status = 'active'", "revoked_at IS NULL", "accepted_at IS NULL"},
			forbidden: []string{"invite_code", "invite_code_hash", "invitee_handle_hash"},
		},
		{
			name:  "select invitation status locks the row and is tenant scoped",
			query: selectAdminInvitationStatusQuery,
			// FOR UPDATE serializes a concurrent revoke without single-threading
			// the whole table; tenant/workspace scope the read.
			mustHave:  []string{"FROM identity_invitations", "tenant_id = $2", "workspace_id = $3", "FOR UPDATE", "tombstoned_at IS NULL"},
			forbidden: []string{"invite_code", "external_group_hash"},
		},
		{
			name:  "grant role assignment upserts on the primary key",
			query: grantAdminRoleAssignmentQuery,
			// ON CONFLICT on the full PK makes a concurrent double grant converge
			// on one row instead of erroring on the active partial index.
			mustHave:  []string{"INSERT INTO identity_membership_roles", "ON CONFLICT (tenant_id, workspace_id, user_id, role_id)", "DO UPDATE", "status = 'active'", "tombstoned_at = NULL", "(xmax = 0)"},
			forbidden: []string{"role_key_hash", "policy_revision_hash AS", "external_group_hash"},
		},
		{
			name:      "revoke role assignment is active-guarded and tenant scoped",
			query:     revokeAdminRoleAssignmentQuery,
			mustHave:  []string{"UPDATE identity_membership_roles", "tenant_id = $1", "workspace_id = $2", "user_id = $3", "role_id = $4", "status = 'active'", "tombstoned_at IS NULL"},
			forbidden: []string{"role_key_hash"},
		},
		{
			name:  "create idp group mapping upserts on the primary key and returns ref",
			query: createAdminIdPGroupMappingQuery,
			mustHave: []string{
				"INSERT INTO identity_provider_group_role_mappings",
				"ON CONFLICT (provider_config_id, external_group_hash, tenant_id, workspace_id, role_id)",
				"DO UPDATE",
				"status = 'active'",
				"md5(",
				"(xmax = 0)",
			},
			// The RETURNING clause emits only the md5 mapping_ref and status; it
			// must never SELECT the raw external_group value (only its hash column
			// participates inside md5()).
			forbidden: []string{"external_group_value", "external_group AS", "AS external_group_hash"},
		},
		{
			name:  "delete idp group mapping resolves md5 ref tenant scoped",
			query: deleteAdminIdPGroupMappingQuery,
			mustHave: []string{
				"UPDATE identity_provider_group_role_mappings",
				"tenant_id = $1",
				"workspace_id = $2",
				"status = 'active'",
				"tombstoned_at IS NULL",
				"md5(",
				"= $4",
			},
			forbidden: []string{"external_group AS", "external_group_value"},
		},
		{
			name:      "select active role is tenant scoped and active guarded",
			query:     selectActiveRoleExistsQuery,
			mustHave:  []string{"FROM identity_roles", "tenant_id = $1", "role_id = $2", "status = 'active'", "tombstoned_at IS NULL"},
			forbidden: []string{"role_key_hash"},
		},
		{
			name:      "select active provider is tenant scoped and active guarded",
			query:     selectActiveProviderExistsQuery,
			mustHave:  []string{"FROM identity_provider_configs", "provider_config_id = $1", "tenant_id = $2", "status = 'active'", "tombstoned_at IS NULL"},
			forbidden: []string{"credential_handle", "issuer_hash", "client_id_hash"},
		},
	}
	for _, tc := range cases {
		for _, want := range tc.mustHave {
			if !strings.Contains(tc.query, want) {
				t.Errorf("%s: query missing %q\n%s", tc.name, want, tc.query)
			}
		}
		for _, bad := range tc.forbidden {
			if strings.Contains(tc.query, bad) {
				t.Errorf("%s: query must not reference %q\n%s", tc.name, bad, tc.query)
			}
		}
		// No raw string interpolation: every write/read must use $N parameters.
		if !strings.Contains(tc.query, "$1") {
			t.Errorf("%s: query is not parameterized", tc.name)
		}
	}
}

// TestAdminMutationsNilDatabase verifies each mutation method returns an error
// (not a panic) when the store has no database.
func TestAdminMutationsNilDatabase(t *testing.T) {
	t.Parallel()

	store := &IdentitySubjectStore{}
	ctx := context.Background()
	if _, err := store.RevokeAdminInvitation(ctx, AdminInvitationRevoke{InviteID: "i", TenantID: "t"}); err == nil {
		t.Error("RevokeAdminInvitation with nil db = nil error, want error")
	}
	if _, err := store.GrantAdminRoleAssignment(ctx, AdminRoleAssignmentGrant{TenantID: "t", UserID: "u", RoleID: "r"}); err == nil {
		t.Error("GrantAdminRoleAssignment with nil db = nil error, want error")
	}
	if _, err := store.RevokeAdminRoleAssignment(ctx, AdminRoleAssignmentRevoke{TenantID: "t", UserID: "u", RoleID: "r"}); err == nil {
		t.Error("RevokeAdminRoleAssignment with nil db = nil error, want error")
	}
	if _, err := store.CreateAdminIdPGroupMapping(ctx, AdminIdPGroupMappingCreate{ProviderConfigID: "p", ExternalGroupHash: "h", TenantID: "t", RoleID: "r"}); err == nil {
		t.Error("CreateAdminIdPGroupMapping with nil db = nil error, want error")
	}
	if _, err := store.DeleteAdminIdPGroupMapping(ctx, AdminIdPGroupMappingDelete{MappingRef: "ref", TenantID: "t"}); err == nil {
		t.Error("DeleteAdminIdPGroupMapping with nil db = nil error, want error")
	}
}

// --- Programmable fake DB driving validation + idempotency branches ---

// adminMutationFakeDB is a programmable ExecQueryer+Beginner that records the
// statements it executes and returns canned rows, so the store's validation and
// idempotency branching can be proven without a real Postgres.
type adminMutationFakeDB struct {
	roleActive     bool
	providerActive bool

	// invitation state for the revoke read-then-write path.
	inviteFound    bool
	inviteStatus   string
	inviteRevoked  bool
	inviteAccepted bool

	upsertInserted bool
	upsertStatus   string
	upsertRef      string

	deleteMatches bool

	execQueries  []string
	queryQueries []string
	tenantArgs   []string
}

func (db *adminMutationFakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execQueries = append(db.execQueries, query)
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			db.tenantArgs = append(db.tenantArgs, s)
		}
	}
	// revoke role assignment affects one row only when an active row exists.
	if strings.Contains(query, "UPDATE identity_membership_roles") {
		if db.deleteMatches {
			return affectedResult{affected: 1}, nil
		}
		return affectedResult{affected: 0}, nil
	}
	// revoke invitation affects one row when active.
	if strings.Contains(query, "UPDATE identity_invitations") {
		return affectedResult{affected: 1}, nil
	}
	return affectedResult{affected: 0}, nil
}

// affectedResult is a sql.Result whose RowsAffected is configurable, unlike the
// shared zero-valued result helper.
type affectedResult struct {
	affected int64
}

func (r affectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r affectedResult) RowsAffected() (int64, error) { return r.affected, nil }

func (db *adminMutationFakeDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.queryQueries = append(db.queryQueries, query)
	switch {
	case strings.Contains(query, "FROM identity_roles"):
		if db.roleActive {
			return &scalarRows{data: [][]any{{1}}}, nil
		}
		return &scalarRows{}, nil
	case strings.Contains(query, "FROM identity_provider_configs"):
		if db.providerActive {
			return &scalarRows{data: [][]any{{1}}}, nil
		}
		return &scalarRows{}, nil
	case strings.Contains(query, "FROM identity_invitations"):
		if !db.inviteFound {
			return &scalarRows{}, nil
		}
		var revoked, accepted any
		if db.inviteRevoked {
			revoked = time.Now().UTC()
		}
		if db.inviteAccepted {
			accepted = time.Now().UTC()
		}
		return &scalarRows{data: [][]any{{db.inviteStatus, revoked, accepted, time.Now().UTC()}}}, nil
	case strings.Contains(query, "INSERT INTO identity_membership_roles"):
		return &scalarRows{data: [][]any{{db.upsertStatus, db.upsertInserted}}}, nil
	case strings.Contains(query, "INSERT INTO identity_provider_group_role_mappings"):
		return &scalarRows{data: [][]any{{db.upsertRef, db.upsertStatus, db.upsertInserted}}}, nil
	case strings.Contains(query, "UPDATE identity_provider_group_role_mappings"):
		if db.deleteMatches {
			return &scalarRows{data: [][]any{{"prov_1"}}}, nil
		}
		return &scalarRows{}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (db *adminMutationFakeDB) Begin(context.Context) (Transaction, error) {
	return &adminMutationFakeTx{db: db}, nil
}

// adminMutationFakeTx delegates to the parent fake so the invitation revoke
// read-then-write path runs against the same canned state.
type adminMutationFakeTx struct {
	db *adminMutationFakeDB
}

func (tx *adminMutationFakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (tx *adminMutationFakeTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.db.QueryContext(ctx, query, args...)
}

func (tx *adminMutationFakeTx) Commit() error   { return nil }
func (tx *adminMutationFakeTx) Rollback() error { return nil }

// scalarRows is a minimal Rows fake supporting *string and *bool scans plus
// nullable time columns scanned as sql.NullTime / *time.Time.
type scalarRows struct {
	data [][]any
	idx  int
}

func (r *scalarRows) Next() bool {
	if r.idx == 0 && r.data == nil {
		return false
	}
	r.idx++
	return r.idx <= len(r.data)
}

func (r *scalarRows) Scan(dest ...any) error {
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *int:
			*d = val.(int)
		case *string:
			if val == nil {
				*d = ""
				continue
			}
			*d = val.(string)
		case *bool:
			*d = val.(bool)
		case *time.Time:
			if val == nil {
				*d = time.Time{}
				continue
			}
			*d = val.(time.Time)
		case *sql.NullTime:
			if val == nil {
				*d = sql.NullTime{}
				continue
			}
			*d = sql.NullTime{Time: val.(time.Time), Valid: true}
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *scalarRows) Err() error   { return nil }
func (r *scalarRows) Close() error { return nil }

// TestGrantAdminRoleAssignmentRejectsUnknownRole proves an unknown/tombstoned
// role is rejected (RoleValid=false) before any membership write.
func TestGrantAdminRoleAssignmentRejectsUnknownRole(t *testing.T) {
	t.Parallel()

	db := &adminMutationFakeDB{roleActive: false}
	store := NewIdentitySubjectStore(db)
	result, err := store.GrantAdminRoleAssignment(context.Background(), AdminRoleAssignmentGrant{
		TenantID: "tenant_a", WorkspaceID: "workspace_a", UserID: "u1", RoleID: "ghost", EffectiveAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("GrantAdminRoleAssignment() error = %v", err)
	}
	if result.RoleValid {
		t.Fatal("RoleValid = true, want false for an unknown role")
	}
	for _, q := range db.queryQueries {
		if strings.Contains(q, "INSERT INTO identity_membership_roles") {
			t.Fatal("membership row was written despite unknown role")
		}
	}
}

// TestGrantAdminRoleAssignmentIdempotent proves a repeated grant reports
// Changed=false (xmax!=0 on conflict) without erroring.
func TestGrantAdminRoleAssignmentIdempotent(t *testing.T) {
	t.Parallel()

	db := &adminMutationFakeDB{roleActive: true, upsertInserted: false, upsertStatus: "active"}
	store := NewIdentitySubjectStore(db)
	result, err := store.GrantAdminRoleAssignment(context.Background(), AdminRoleAssignmentGrant{
		TenantID: "tenant_a", WorkspaceID: "workspace_a", UserID: "u1", RoleID: "developer", EffectiveAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("GrantAdminRoleAssignment() error = %v", err)
	}
	if !result.RoleValid || result.Changed {
		t.Fatalf("re-grant = %+v, want RoleValid=true Changed=false", result)
	}
}

// TestCreateAdminIdPGroupMappingValidatesProviderAndRole proves an unknown
// provider or role is rejected before any mapping write, and a valid create
// returns the mapping_ref.
func TestCreateAdminIdPGroupMappingValidatesProviderAndRole(t *testing.T) {
	t.Parallel()

	base := AdminIdPGroupMappingCreate{
		ProviderConfigID: "prov_1", ExternalGroupHash: "sha256:abc", TenantID: "tenant_a",
		WorkspaceID: "workspace_a", RoleID: "developer", EffectiveAt: time.Now(),
	}

	// Unknown provider.
	db := &adminMutationFakeDB{providerActive: false, roleActive: true}
	res, err := NewIdentitySubjectStore(db).CreateAdminIdPGroupMapping(context.Background(), base)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if res.ProviderValid {
		t.Fatal("ProviderValid = true, want false for unknown provider")
	}

	// Known provider, unknown role.
	db = &adminMutationFakeDB{providerActive: true, roleActive: false}
	res, err = NewIdentitySubjectStore(db).CreateAdminIdPGroupMapping(context.Background(), base)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !res.ProviderValid || res.RoleValid {
		t.Fatalf("res = %+v, want ProviderValid=true RoleValid=false", res)
	}

	// Both valid: returns ref.
	db = &adminMutationFakeDB{providerActive: true, roleActive: true, upsertInserted: true, upsertStatus: "active", upsertRef: "ref_abc"}
	res, err = NewIdentitySubjectStore(db).CreateAdminIdPGroupMapping(context.Background(), base)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !res.ProviderValid || !res.RoleValid || res.MappingRef != "ref_abc" {
		t.Fatalf("res = %+v, want valid with ref_abc", res)
	}
}

// TestRevokeAdminInvitationIdempotent proves an already-revoked invitation is a
// safe no-op (Found=true, Revoked=false) and a missing one is Found=false, with
// no UPDATE issued in either case.
func TestRevokeAdminInvitationIdempotent(t *testing.T) {
	t.Parallel()

	// Already revoked.
	db := &adminMutationFakeDB{inviteFound: true, inviteStatus: "revoked", inviteRevoked: true}
	res, err := NewIdentitySubjectStore(db).RevokeAdminInvitation(context.Background(), AdminInvitationRevoke{
		InviteID: "inv_1", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevokedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !res.Found || res.Revoked {
		t.Fatalf("re-revoke = %+v, want Found=true Revoked=false", res)
	}
	for _, q := range db.execQueries {
		if strings.Contains(q, "UPDATE identity_invitations") {
			t.Fatal("UPDATE issued against an already-revoked invitation")
		}
	}

	// Missing invitation.
	db = &adminMutationFakeDB{inviteFound: false}
	res, err = NewIdentitySubjectStore(db).RevokeAdminInvitation(context.Background(), AdminInvitationRevoke{
		InviteID: "missing", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevokedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if res.Found {
		t.Fatal("Found = true, want false for a missing invitation")
	}

	// Active invitation: revoked.
	db = &adminMutationFakeDB{inviteFound: true, inviteStatus: "active"}
	res, err = NewIdentitySubjectStore(db).RevokeAdminInvitation(context.Background(), AdminInvitationRevoke{
		InviteID: "inv_1", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevokedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !res.Found || !res.Revoked || res.Status != "revoked" {
		t.Fatalf("active revoke = %+v, want Found=true Revoked=true status=revoked", res)
	}
}

// TestRevokeAdminRoleAssignmentIdempotent proves a revoke of an absent active
// row reports Changed=false without error.
func TestRevokeAdminRoleAssignmentIdempotent(t *testing.T) {
	t.Parallel()

	db := &adminMutationFakeDB{deleteMatches: false}
	res, err := NewIdentitySubjectStore(db).RevokeAdminRoleAssignment(context.Background(), AdminRoleAssignmentRevoke{
		TenantID: "tenant_a", WorkspaceID: "workspace_a", UserID: "u1", RoleID: "developer", RevokedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if res.Changed {
		t.Fatal("Changed = true, want false for an already-revoked assignment")
	}
}

// TestDeleteAdminIdPGroupMappingIdempotent proves a delete that matches no
// active mapping reports Found=false without error, and a match reports Deleted.
func TestDeleteAdminIdPGroupMappingIdempotent(t *testing.T) {
	t.Parallel()

	db := &adminMutationFakeDB{deleteMatches: false}
	res, err := NewIdentitySubjectStore(db).DeleteAdminIdPGroupMapping(context.Background(), AdminIdPGroupMappingDelete{
		MappingRef: "ref_x", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevokedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if res.Found || res.Deleted {
		t.Fatalf("no-match delete = %+v, want Found=false Deleted=false", res)
	}

	db = &adminMutationFakeDB{deleteMatches: true}
	res, err = NewIdentitySubjectStore(db).DeleteAdminIdPGroupMapping(context.Background(), AdminIdPGroupMappingDelete{
		MappingRef: "ref_abc", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevokedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !res.Found || !res.Deleted {
		t.Fatalf("matched delete = %+v, want Found=true Deleted=true", res)
	}
}
