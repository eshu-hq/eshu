// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"reflect"
	"sort"
	"testing"
)

// frozenRetirementAdmission pins the exact method set behind the
// versioned #8872 admission contract for the operator retirement trigger.
// A renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump RetirementAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently.
var frozenRetirementAdmission = []string{
	"TriggerRetirement func(context.Context, retirement.TriggerRequest) (retirement.TriggerResult, error)",
}

// retirementAdmissionMethodSet reflects the live RetirementAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func retirementAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*RetirementAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestRetirementAdmissionContractIsVersioned pins the #8872
// admission-contract version for the operator repository retirement
// trigger (POST /api/v0/admin/repository-retirements). The version value
// plus the frozen method set below are the enforcement: a breaking change
// to the trigger shape fails this test until the version bumps in the
// same diff.
func TestRetirementAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if RetirementAdmissionContractVersion != "v0" {
		t.Errorf("RetirementAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", RetirementAdmissionContractVersion, "v0")
	}
	got := retirementAdmissionMethodSet(t)
	if len(got) != len(frozenRetirementAdmission) {
		t.Fatalf("RetirementAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenRetirementAdmission, len(frozenRetirementAdmission), RetirementAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenRetirementAdmission[i] {
			t.Errorf("RetirementAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenRetirementAdmission[i], RetirementAdmissionContractVersion)
		}
	}
}
