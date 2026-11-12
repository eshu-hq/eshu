// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenRetireUnclaimedAdmission pins the exact method set behind the
// versioned #9669 writer admission contract for the never-claimed workflow
// retirement the admin workflow-retire-unclaimed endpoint admits through
// Store. A renamed, added, removed, or re-signed method fails here until
// that list is deliberately updated; bump
// RetireUnclaimedAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can pin
// the shape, keeping the eshu-api extraction boundary from drifting
// silently.
var frozenRetireUnclaimedAdmission = []string{
	"RetireUnclaimedWorkflowItems func(context.Context, string, string, string) ([]admin.RetiredUnclaimedItem, error)",
}

// retireUnclaimedAdmissionMethodSet reflects the live
// RetireUnclaimedAdmission method set into sorted "name signature" strings
// for comparison with the frozen list.
func retireUnclaimedAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*RetireUnclaimedAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestRetireUnclaimedAdmissionContractIsVersioned pins the #9669 writer
// admission-contract version for the never-claimed workflow retirement
// (POST /api/v0/admin/workflow-retire-unclaimed admits through
// Store.RetireUnclaimedWorkflowItems). The version value plus the frozen
// method set below are the enforcement: a breaking change to the admission
// shape fails this test until the version bumps in the same diff.
func TestRetireUnclaimedAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if RetireUnclaimedAdmissionContractVersion != "v0" {
		t.Errorf("RetireUnclaimedAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", RetireUnclaimedAdmissionContractVersion, "v0")
	}
	got := retireUnclaimedAdmissionMethodSet(t)
	if len(got) != len(frozenRetireUnclaimedAdmission) {
		t.Fatalf("RetireUnclaimedAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenRetireUnclaimedAdmission, len(frozenRetireUnclaimedAdmission), RetireUnclaimedAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenRetireUnclaimedAdmission[i] {
			t.Errorf("RetireUnclaimedAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenRetireUnclaimedAdmission[i], RetireUnclaimedAdmissionContractVersion)
		}
	}
}
