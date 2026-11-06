// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"reflect"
	"sort"
	"testing"
)

// frozenAdminIdentityReadAdmission pins the exact method set behind the
// versioned #9669 D1 admission contract for tenant-scoped admin
// identity reads. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// AdminIdentityReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// D-wave consumer: D3 (admin/identity/recovery reads, per the arbiter
// D-wave ruling on #9669).
var frozenAdminIdentityReadAdmission = []string{
	"ListAdminAPITokens func(context.Context, string, string) ([]identity.APITokenListItem, error)",
	"ListAdminIdPGroupMappings func(context.Context, string, string, string) ([]identity.IdPGroupMappingListItem, error)",
	"ListAdminIdPProviders func(context.Context, string) ([]identity.IdPProviderListItem, error)",
	"ListAdminInvitations func(context.Context, string, string) ([]identity.InvitationListItem, error)",
	"ListAdminRoleAssignments func(context.Context, string, string, string) ([]identity.RoleAssignmentListItem, error)",
	"ListAdminRoles func(context.Context, string) ([]identity.RoleListItem, bool, error)",
}

// adminIdentityReadAdmissionMethodSet reflects the live
// AdminIdentityReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func adminIdentityReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*AdminIdentityReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestAdminIdentityReadAdmissionContractIsVersioned pins the #9669 D1
// admission-contract version for tenant-scoped admin identity reads
// (invitation, role-assignment, role, IdP provider, IdP group-mapping,
// and API-token lists). The version value plus the frozen method set
// below are the enforcement: a breaking change to the admission shape
// fails this test until the version bumps in the same diff.
func TestAdminIdentityReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if AdminIdentityReadAdmissionContractVersion != "v0" {
		t.Errorf("AdminIdentityReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", AdminIdentityReadAdmissionContractVersion, "v0")
	}
	got := adminIdentityReadAdmissionMethodSet(t)
	if len(got) != len(frozenAdminIdentityReadAdmission) {
		t.Fatalf("AdminIdentityReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenAdminIdentityReadAdmission, len(frozenAdminIdentityReadAdmission), AdminIdentityReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenAdminIdentityReadAdmission[i] {
			t.Errorf("AdminIdentityReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenAdminIdentityReadAdmission[i], AdminIdentityReadAdmissionContractVersion)
		}
	}
}
