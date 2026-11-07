// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenSkipAdmission pins the exact method set behind the versioned #9669
// D7 admission contract for the operator skip mutation the admin skip
// endpoint admits through Store. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// SkipAdmissionContractVersion alongside (manually — the bump is convention,
// the frozen list is the enforcement) so dest consumers can pin the shape,
// keeping the eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: D7 (admin four, per the arbiter D-wave ruling on #9669).
var frozenSkipAdmission = []string{
	"SkipRepositoryWorkItems func(context.Context, string, string) ([]admin.WorkItem, error)",
}

// skipAdmissionMethodSet reflects the live SkipAdmission method set into
// sorted "name signature" strings for comparison with the frozen list.
func skipAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SkipAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSkipAdmissionContractIsVersioned pins the #9669 D7 admission-contract
// version for the operator skip mutation (POST /api/v0/admin/skip admits
// through Store.SkipRepositoryWorkItems). The version value plus the frozen
// method set below are the enforcement: a breaking change to the admission
// shape fails this test until the version bumps in the same diff.
func TestSkipAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SkipAdmissionContractVersion != "v0" {
		t.Errorf("SkipAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SkipAdmissionContractVersion, "v0")
	}
	got := skipAdmissionMethodSet(t)
	if len(got) != len(frozenSkipAdmission) {
		t.Fatalf("SkipAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSkipAdmission, len(frozenSkipAdmission), SkipAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSkipAdmission[i] {
			t.Errorf("SkipAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSkipAdmission[i], SkipAdmissionContractVersion)
		}
	}
}
