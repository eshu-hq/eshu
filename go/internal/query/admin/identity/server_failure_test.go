// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// failingReadStore fails every admin identity read with err.
type failingReadStore struct{ err error }

func (s failingReadStore) ListAdminInvitations(context.Context, string, string) ([]InvitationListItem, error) {
	return nil, s.err
}

func (s failingReadStore) ListAdminRoleAssignments(context.Context, string, string, string) ([]RoleAssignmentListItem, error) {
	return nil, s.err
}

func (s failingReadStore) ListAdminRoles(context.Context, string) ([]RoleListItem, bool, error) {
	return nil, false, s.err
}

func (s failingReadStore) ListAdminIdPProviders(context.Context, string) ([]IdPProviderListItem, error) {
	return nil, s.err
}

func (s failingReadStore) ListAdminIdPGroupMappings(context.Context, string, string, string) ([]IdPGroupMappingListItem, error) {
	return nil, s.err
}

func (s failingReadStore) ListAdminAPITokens(context.Context, string, string) ([]APITokenListItem, error) {
	return nil, s.err
}

// failingMutationStore fails every admin identity mutation with err.
type failingMutationStore struct{ err error }

func (s failingMutationStore) RevokeAdminInvitation(context.Context, InvitationRevokeRequest) (InvitationRevokeResult, error) {
	return InvitationRevokeResult{}, s.err
}

func (s failingMutationStore) GrantAdminRoleAssignment(context.Context, RoleAssignmentGrantRequest) (RoleAssignmentMutationResult, error) {
	return RoleAssignmentMutationResult{}, s.err
}

func (s failingMutationStore) RevokeAdminRoleAssignment(context.Context, RoleAssignmentRevokeRequest) (RoleAssignmentMutationResult, error) {
	return RoleAssignmentMutationResult{}, s.err
}

func (s failingMutationStore) CreateAdminIdPGroupMapping(context.Context, IdPGroupMappingCreateRequest) (IdPGroupMappingCreateResult, error) {
	return IdPGroupMappingCreateResult{}, s.err
}

func (s failingMutationStore) DeleteAdminIdPGroupMapping(context.Context, IdPGroupMappingDeleteRequest) (IdPGroupMappingDeleteResult, error) {
	return IdPGroupMappingDeleteResult{}, s.err
}

// tenantAdminScope adds an all-scope tenant admin to the request context.
func tenantAdminScope(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, allScopeAdminAuth("tenant_a", "workspace_a"))
}

// runFailureSteps drives every route through the fault, client-cancel, and
// inner-cancel cases. The identity reads and mutations run on the writer, so
// the reader-fence case does not apply. The two audit reads go through the
// guarded reader and run every case, the fence included, in
// audit_failure_test.go.
func runFailureSteps(t *testing.T, suite failure.Suite) {
	t.Helper()
	for _, route := range suite.Routes {
		for _, tc := range failure.Cases() {
			if tc.Kind == failure.ReaderFence {
				continue
			}
			t.Run(route.Name+"/"+tc.Name, func(t *testing.T) {
				if err := failure.Check(suite, route, tc); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

// TestAdminIdentityReadFailuresAnswerFixedText drives every admin identity
// read through a backend fault and a client cancel (#7674). The audit reads
// are covered in audit_failure_test.go.
func TestAdminIdentityReadFailuresAnswerFixedText(t *testing.T) {
	reads := func(err error) failure.Mounter {
		return &ReadHandler{Store: failingReadStore{err: err}}
	}
	routes := []failure.Route{
		{Name: "invitations", Path: "/api/v0/auth/local/invitations", Message: invitationListFailedMessage},
		{Name: "role assignments", Path: "/api/v0/auth/admin/role-assignments", Message: roleAssignmentListFailedMessage},
		{Name: "roles", Path: "/api/v0/auth/admin/roles", Message: roleListFailedMessage},
		{Name: "idp providers", Path: "/api/v0/auth/admin/idp-providers", Message: idpProviderListFailedMessage},
		{Name: "idp group mappings", Path: "/api/v0/auth/admin/idp-group-mappings", Message: idpGroupMappingListFailedMessage},
		{Name: "api tokens", Path: "/api/v0/auth/admin/api-tokens", Message: apiAccessKeyListFailedMessage},
	}
	for i := range routes {
		routes[i].Handler = reads
		routes[i].Scope = tenantAdminScope
	}
	runFailureSteps(t, failure.Suite{Routes: routes})
}

// TestAdminIdentityMutationFailuresAnswerFixedText drives every admin identity
// mutation through a backend fault and a client cancel (#7674).
func TestAdminIdentityMutationFailuresAnswerFixedText(t *testing.T) {
	mutations := func(err error) failure.Mounter {
		return &MutationHandler{Store: failingMutationStore{err: err}, Audit: &testutil.FakeGovernanceAuditAppender{}}
	}
	routes := []failure.Route{
		{
			Name: "revoke invitation", Method: http.MethodPost,
			Path: "/api/v0/auth/local/invitations/inv_1/revoke", Message: invitationRevokeFailedMessage,
		},
		{
			Name: "grant role assignment", Method: http.MethodPost, Path: "/api/v0/auth/admin/role-assignments",
			Body: `{"user_id":"u1","role_id":"developer"}`, Message: roleAssignmentGrantFailedMessage,
		},
		{
			Name: "revoke role assignment", Method: http.MethodPost, Path: "/api/v0/auth/admin/role-assignments/revoke",
			Body: `{"user_id":"u1","role_id":"developer"}`, Message: roleAssignmentRevokeFailedMessage,
		},
		{
			Name: "create idp group mapping", Method: http.MethodPost, Path: "/api/v0/auth/admin/idp-group-mappings",
			Body:    `{"provider_config_id":"prov_1","external_group":"Eshu Developers","role_id":"developer"}`,
			Message: idpGroupMappingCreateFailedMessage,
		},
		{
			Name: "delete idp group mapping", Method: http.MethodDelete,
			Path: "/api/v0/auth/admin/idp-group-mappings/ref_abc", Message: idpGroupMappingDeleteFailedMessage,
		},
	}
	for i := range routes {
		routes[i].Handler = mutations
		routes[i].Scope = tenantAdminScope
	}
	runFailureSteps(t, failure.Suite{Routes: routes})
}
