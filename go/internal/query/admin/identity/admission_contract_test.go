// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"reflect"
	"sort"
	"testing"
)

// frozenAdminIdentityAdmission pins the exact method set behind the
// versioned #8872 admission contract for tenant-scoped admin identity
// mutations. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// AdminIdentityAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can
// pin the shape, keeping the eshu-api extraction boundary from drifting
// silently.
var frozenAdminIdentityAdmission = []string{
	"CreateAdminIdPGroupMapping func(context.Context, identity.IdPGroupMappingCreateRequest) (identity.IdPGroupMappingCreateResult, error)",
	"DeleteAdminIdPGroupMapping func(context.Context, identity.IdPGroupMappingDeleteRequest) (identity.IdPGroupMappingDeleteResult, error)",
	"GrantAdminRoleAssignment func(context.Context, identity.RoleAssignmentGrantRequest) (identity.RoleAssignmentMutationResult, error)",
	"RevokeAdminInvitation func(context.Context, identity.InvitationRevokeRequest) (identity.InvitationRevokeResult, error)",
	"RevokeAdminRoleAssignment func(context.Context, identity.RoleAssignmentRevokeRequest) (identity.RoleAssignmentMutationResult, error)",
}

// adminIdentityAdmissionMethodSet reflects the live AdminIdentityAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func adminIdentityAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*AdminIdentityAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestAdminIdentityAdmissionContractIsVersioned pins the #8872
// admission-contract version for tenant-scoped admin identity mutations
// (invitation revoke, role grant/revoke, IdP group mapping create/delete).
// The version value plus the frozen method set below are the enforcement:
// a breaking change to the mutation shape fails this test until the
// version bumps in the same diff.
func TestAdminIdentityAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if AdminIdentityAdmissionContractVersion != "v0" {
		t.Errorf("AdminIdentityAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", AdminIdentityAdmissionContractVersion, "v0")
	}
	got := adminIdentityAdmissionMethodSet(t)
	if len(got) != len(frozenAdminIdentityAdmission) {
		t.Fatalf("AdminIdentityAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenAdminIdentityAdmission, len(frozenAdminIdentityAdmission), AdminIdentityAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenAdminIdentityAdmission[i] {
			t.Errorf("AdminIdentityAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenAdminIdentityAdmission[i], AdminIdentityAdmissionContractVersion)
		}
	}
}
