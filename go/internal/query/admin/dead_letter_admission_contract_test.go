// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenDeadLetterAdmission pins the exact method set behind the versioned
// #9669 D7 admission contract for the operator dead-letter mutation the
// admin dead-letter endpoint admits through Store. A renamed, added,
// removed, or re-signed method fails here until that list is deliberately
// updated; bump DeadLetterAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// D-wave consumer: D7 (admin four, per the arbiter D-wave ruling on #9669).
var frozenDeadLetterAdmission = []string{
	"DeadLetterWorkItems func(context.Context, admin.DeadLetterFilter) ([]admin.WorkItem, error)",
}

// deadLetterAdmissionMethodSet reflects the live DeadLetterAdmission method
// set into sorted "name signature" strings for comparison with the frozen
// list.
func deadLetterAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*DeadLetterAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestDeadLetterAdmissionContractIsVersioned pins the #9669 D7
// admission-contract version for the operator dead-letter mutation (POST
// /api/v0/admin/dead-letter admits through Store.DeadLetterWorkItems). The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestDeadLetterAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if DeadLetterAdmissionContractVersion != "v0" {
		t.Errorf("DeadLetterAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", DeadLetterAdmissionContractVersion, "v0")
	}
	got := deadLetterAdmissionMethodSet(t)
	if len(got) != len(frozenDeadLetterAdmission) {
		t.Fatalf("DeadLetterAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenDeadLetterAdmission, len(frozenDeadLetterAdmission), DeadLetterAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenDeadLetterAdmission[i] {
			t.Errorf("DeadLetterAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenDeadLetterAdmission[i], DeadLetterAdmissionContractVersion)
		}
	}
}
