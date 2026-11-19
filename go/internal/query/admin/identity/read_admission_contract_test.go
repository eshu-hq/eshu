// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"
	"reflect"
	"sort"
	"strings"
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
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func adminIdentityReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
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
	got := adminIdentityReadAdmissionMethodSet(t, (*AdminIdentityReadAdmission)(nil))
	if len(got) != len(frozenAdminIdentityReadAdmission) {
		t.Fatalf("AdminIdentityReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenAdminIdentityReadAdmission, len(frozenAdminIdentityReadAdmission), AdminIdentityReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenAdminIdentityReadAdmission[i] {
			t.Errorf("AdminIdentityReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenAdminIdentityReadAdmission[i], AdminIdentityReadAdmissionContractVersion)
		}
	}
}

// TestAdminIdentityReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the six re-declared identity reads, with no
// mutation, revocation, or other-family surface riding along. The
// frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands.
func TestAdminIdentityReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*AdminIdentityReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 6 {
		t.Fatalf("AdminIdentityReadAdmission has %d methods, want exactly 6: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Create", "Update", "Delete", "Revoke", "Write", "Upsert", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("AdminIdentityReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededAdminIdentityDrift is a deliberately widened copy of the pin:
// the six frozen identity reads plus a token-revocation write, which
// the identity-read lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededAdminIdentityDrift interface {
	AdminIdentityReadAdmission
	RevokeAdminAPIToken(context.Context, string) error
}

// TestAdminIdentityReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestAdminIdentityReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := adminIdentityReadAdmissionMethodSet(t, (*seededAdminIdentityDrift)(nil))
	if reflect.DeepEqual(got, frozenAdminIdentityReadAdmission) {
		t.Fatal("seeded widening (RevokeAdminAPIToken) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenAdminIdentityReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenAdminIdentityReadAdmission)+1)
	}
}
