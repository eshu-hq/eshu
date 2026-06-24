package postgres

import (
	"strings"
	"testing"
)

// adminReadQueryCase pairs an admin read-list SQL string with the columns it
// must select and the secret/hash columns it must never select.
type adminReadQueryCase struct {
	name      string
	query     string
	want      []string
	forbidden []string
	params    []string
}

// TestAdminIdentityReadQueriesSecurity verifies every admin identity read-list
// query selects only metadata columns, is tenant-scoped by parameter, is
// bounded by LIMIT, and never selects a hashed secret, invite code, credential
// handle, or external group hash.
func TestAdminIdentityReadQueriesSecurity(t *testing.T) {
	t.Parallel()

	cases := []adminReadQueryCase{
		{
			name:  "invitations",
			query: listAdminInvitationsQuery,
			want:  []string{"invite_id", "role_id", "status", "expires_at", "accepted_at", "revoked_at"},
			forbidden: []string{
				"invite_code_hash", "invitee_handle_hash", "inviter_subject_id_hash",
			},
			params: []string{"$1", "$2"},
		},
		{
			name:      "role_assignments",
			query:     listAdminRoleAssignmentsQuery,
			want:      []string{"user_id", "role_id", "assignment_source", "status", "effective_at", "expires_at"},
			forbidden: []string{"policy_revision_hash"},
			params:    []string{"$1", "$2", "$3"},
		},
		{
			name:      "roles",
			query:     listAdminRolesQuery,
			want:      []string{"role_id", "status", "built_in"},
			forbidden: []string{"role_key_hash", "policy_revision_hash"},
			params:    []string{"$1"},
		},
		{
			name:      "role_grants",
			query:     listAdminRoleGrantsQuery,
			want:      []string{"role_id", "grant_id", "action", "feature", "data_class", "scope_class", "status"},
			forbidden: []string{"scope_id_hash", "repository_id_hash", "policy_revision_hash"},
			params:    []string{"$1"},
		},
		{
			name:  "idp_providers",
			query: listAdminIdPProvidersQuery,
			want:  []string{"provider_config_id", "provider_kind", "status"},
			forbidden: []string{
				"provider_key_hash", "issuer_hash", "metadata_url_hash",
				"entity_id_hash", "client_id_hash", "credential_handle",
			},
			params: []string{"$1"},
		},
		{
			name:      "idp_group_mappings",
			query:     listAdminIdPGroupMappingsQuery,
			want:      []string{"mapping_ref", "provider_config_id", "role_id", "status", "effective_at", "expires_at"},
			forbidden: []string{"policy_revision_hash"},
			params:    []string{"$1", "$2"},
		},
		{
			name:      "api_tokens",
			query:     listAdminAPITokensQuery,
			want:      []string{"token_id", "token_class", "user_id", "service_principal_id", "status", "issued_at", "expires_at", "revoked_at"},
			forbidden: []string{"token_hash", "display_handle_hash"},
			params:    []string{"$1", "$2"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.want {
				if !strings.Contains(tc.query, want) {
					t.Errorf("%s query missing column %q", tc.name, want)
				}
			}
			for _, forbidden := range tc.forbidden {
				if strings.Contains(tc.query, forbidden) {
					t.Errorf("%s query must not expose secret column %q", tc.name, forbidden)
				}
			}
			// The external_group_hash column is the hashed group-name secret. It
			// may appear only inside the mapping_ref md5() digest input (which is
			// one-way and opaque) and must never be SELECTed as a bare output
			// column. Assert it is not selected bare in any form.
			if tc.name == "idp_group_mappings" {
				if strings.Contains(tc.query, "external_group_hash,") ||
					strings.Contains(tc.query, "external_group_hash AS") ||
					strings.Contains(tc.query, "external_group_hash\n") {
					t.Errorf("idp_group_mappings query must not select external_group_hash as an output column")
				}
				// The only permitted occurrence is inside md5(... external_group_hash).
				if strings.Contains(tc.query, "external_group_hash") &&
					!strings.Contains(tc.query, "external_group_hash) AS mapping_ref") {
					t.Errorf("idp_group_mappings query exposes external_group_hash outside the mapping_ref digest")
				}
			}
			for _, param := range tc.params {
				if !strings.Contains(tc.query, param) {
					t.Errorf("%s query missing bind parameter %q", tc.name, param)
				}
			}
			if !strings.Contains(tc.query, "LIMIT") {
				t.Errorf("%s query must be bounded by LIMIT", tc.name)
			}
			if !strings.Contains(tc.query, "tenant_id = $1") {
				t.Errorf("%s query must scope by tenant_id = $1", tc.name)
			}
		})
	}
}

// TestAdminIdentityReadsNilDatabase verifies every admin read returns an error
// when the store has no database handle.
func TestAdminIdentityReadsNilDatabase(t *testing.T) {
	t.Parallel()

	store := &IdentitySubjectStore{db: nil}
	if _, err := store.ListAdminInvitations(nil, "tenant", "workspace"); err == nil { //nolint:staticcheck
		t.Error("ListAdminInvitations: expected error for nil database")
	}
	if _, err := store.ListAdminRoleAssignments(nil, "tenant", "workspace", ""); err == nil { //nolint:staticcheck
		t.Error("ListAdminRoleAssignments: expected error for nil database")
	}
	if _, err := store.ListAdminRoles(nil, "tenant"); err == nil { //nolint:staticcheck
		t.Error("ListAdminRoles: expected error for nil database")
	}
	if _, err := store.ListAdminIdPProviders(nil, "tenant"); err == nil { //nolint:staticcheck
		t.Error("ListAdminIdPProviders: expected error for nil database")
	}
	if _, err := store.ListAdminIdPGroupMappings(nil, "tenant", "workspace"); err == nil { //nolint:staticcheck
		t.Error("ListAdminIdPGroupMappings: expected error for nil database")
	}
	if _, err := store.ListAdminAPITokens(nil, "tenant", "workspace"); err == nil { //nolint:staticcheck
		t.Error("ListAdminAPITokens: expected error for nil database")
	}
}

// TestAdminIdentityReadsRejectBlankTenant verifies every admin read rejects a
// blank tenant id before touching the database, so an empty AuthContext can
// never list across tenants.
func TestAdminIdentityReadsRejectBlankTenant(t *testing.T) {
	t.Parallel()

	store := &IdentitySubjectStore{db: &fakeExecQueryer{}}
	if _, err := store.ListAdminInvitations(nil, "  ", "workspace"); err == nil { //nolint:staticcheck
		t.Error("ListAdminInvitations: expected error for blank tenant")
	}
	if _, err := store.ListAdminRoleAssignments(nil, "", "workspace", ""); err == nil { //nolint:staticcheck
		t.Error("ListAdminRoleAssignments: expected error for blank tenant")
	}
	if _, err := store.ListAdminRoles(nil, ""); err == nil { //nolint:staticcheck
		t.Error("ListAdminRoles: expected error for blank tenant")
	}
	if _, err := store.ListAdminIdPProviders(nil, ""); err == nil { //nolint:staticcheck
		t.Error("ListAdminIdPProviders: expected error for blank tenant")
	}
	if _, err := store.ListAdminIdPGroupMappings(nil, "", "workspace"); err == nil { //nolint:staticcheck
		t.Error("ListAdminIdPGroupMappings: expected error for blank tenant")
	}
	if _, err := store.ListAdminAPITokens(nil, "", "workspace"); err == nil { //nolint:staticcheck
		t.Error("ListAdminAPITokens: expected error for blank tenant")
	}
}
