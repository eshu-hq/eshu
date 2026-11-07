// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenBackfillAdmission pins the exact method set behind the versioned
// #9669 D7 admission contract for the operator backfill mutation the admin
// backfill endpoint admits through Store. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated; bump
// BackfillAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can pin
// the shape, keeping the eshu-api extraction boundary from drifting
// silently.
//
// D-wave consumer: D7 (admin four, per the arbiter D-wave ruling on #9669).
var frozenBackfillAdmission = []string{
	"RequestBackfill func(context.Context, admin.BackfillInput) (*admin.BackfillRequest, error)",
}

// backfillAdmissionMethodSet reflects the live BackfillAdmission method set
// into sorted "name signature" strings for comparison with the frozen list.
func backfillAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*BackfillAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestBackfillAdmissionContractIsVersioned pins the #9669 D7
// admission-contract version for the operator backfill mutation (POST
// /api/v0/admin/backfill admits through Store.RequestBackfill). The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestBackfillAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if BackfillAdmissionContractVersion != "v0" {
		t.Errorf("BackfillAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", BackfillAdmissionContractVersion, "v0")
	}
	got := backfillAdmissionMethodSet(t)
	if len(got) != len(frozenBackfillAdmission) {
		t.Fatalf("BackfillAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenBackfillAdmission, len(frozenBackfillAdmission), BackfillAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenBackfillAdmission[i] {
			t.Errorf("BackfillAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenBackfillAdmission[i], BackfillAdmissionContractVersion)
		}
	}
}
