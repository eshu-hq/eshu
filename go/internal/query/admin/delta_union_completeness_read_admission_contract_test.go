// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenDeltaUnionCompletenessReadAdmission pins the exact method set
// behind the versioned #9669 Gap 02 admission contract for the
// delta-union completeness read (GET
// /api/v0/admin/delta-union/completeness via
// DeltaUnionCompletenessChecker.Check). A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump DeltaUnionCompletenessReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: D3 (admin reads, per the arbiter D-wave ruling on
// #9669: Decision 3 / census Q4 — admin reads go native only after the
// pin).
var frozenDeltaUnionCompletenessReadAdmission = []string{
	"Check func(context.Context, []string) (recovery.DeltaUnionCompletenessReport, error)",
}

// deltaUnionCompletenessReadAdmissionMethodSet reflects the live
// DeltaUnionCompletenessReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func deltaUnionCompletenessReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*DeltaUnionCompletenessReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestDeltaUnionCompletenessReadAdmissionContractIsVersioned pins the
// #9669 Gap 02 admission-contract version for the delta-union
// completeness read. The version value plus the frozen method set below
// are the enforcement: a breaking change to the admission shape fails
// this test until the version bumps in the same diff.
func TestDeltaUnionCompletenessReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if DeltaUnionCompletenessReadAdmissionContractVersion != "v0" {
		t.Errorf("DeltaUnionCompletenessReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", DeltaUnionCompletenessReadAdmissionContractVersion, "v0")
	}
	got := deltaUnionCompletenessReadAdmissionMethodSet(t)
	if len(got) != len(frozenDeltaUnionCompletenessReadAdmission) {
		t.Fatalf("DeltaUnionCompletenessReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenDeltaUnionCompletenessReadAdmission, len(frozenDeltaUnionCompletenessReadAdmission), DeltaUnionCompletenessReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenDeltaUnionCompletenessReadAdmission[i] {
			t.Errorf("DeltaUnionCompletenessReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenDeltaUnionCompletenessReadAdmission[i], DeltaUnionCompletenessReadAdmissionContractVersion)
		}
	}
}
