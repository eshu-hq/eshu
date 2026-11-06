// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenGovernanceAuditAdmission pins the exact method set behind the
// versioned admission contract for governance-audit append. A renamed,
// added, removed, or re-signed method fails here until that list is
// deliberately updated; bump GovernanceAuditAdmissionContractVersion
// alongside so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently. The list holds exactly one
// append method: the admission seam is append-only, with no read surface
// (no Summary, no List), so a dest client cannot acquire a read it did
// not bargain for through this contract.
var frozenGovernanceAuditAdmission = []string{
	"Append func(context.Context, []governanceaudit.Event) error",
}

// governanceAuditAdmissionMethodSet reflects the live
// GovernanceAuditAdmission method set into sorted "name signature" strings
// for comparison with the frozen list.
func governanceAuditAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*GovernanceAuditAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestGovernanceAuditAdmissionContractIsVersioned pins the admission-contract
// version for governance-audit append. The version value plus the frozen
// method set below are the enforcement: a breaking change to the admission
// shape fails this test until the frozen list is deliberately updated and
// the version bumps in the same diff.
func TestGovernanceAuditAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if GovernanceAuditAdmissionContractVersion != "v0" {
		t.Errorf("GovernanceAuditAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", GovernanceAuditAdmissionContractVersion, "v0")
	}
	got := governanceAuditAdmissionMethodSet(t)
	if len(got) != len(frozenGovernanceAuditAdmission) {
		t.Fatalf("GovernanceAuditAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenGovernanceAuditAdmission, len(frozenGovernanceAuditAdmission), GovernanceAuditAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenGovernanceAuditAdmission[i] {
			t.Errorf("GovernanceAuditAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenGovernanceAuditAdmission[i], GovernanceAuditAdmissionContractVersion)
		}
	}
}
