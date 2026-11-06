// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"reflect"
	"sort"
	"testing"
)

// frozenStatusReadAdmission pins the exact method set behind the
// versioned #9669 D6 admission contract for the Status family reads
// the status endpoints admit through Reader. A renamed, added,
// removed, or re-signed method fails here until that list is
// deliberately updated; bump StatusReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669).
var frozenStatusReadAdmission = []string{
	"ReadStatusSnapshot func(context.Context, time.Time) (status.RawSnapshot, error)",
	"ReadStatusSnapshotFiltered func(context.Context, time.Time, status.SnapshotSelection) (status.RawSnapshot, error)",
}

// statusReadAdmissionMethodSet reflects the live StatusReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list.
func statusReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*StatusReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestStatusReadAdmissionContractIsVersioned pins the #9669 D6
// admission-contract version for the Status family reads (the status
// endpoints admit through Reader). The version value plus the frozen
// method set below are the enforcement: a breaking change to the
// admission shape fails this test until the version bumps in the same
// diff.
func TestStatusReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if StatusReadAdmissionContractVersion != "v0" {
		t.Errorf("StatusReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", StatusReadAdmissionContractVersion, "v0")
	}
	got := statusReadAdmissionMethodSet(t)
	if len(got) != len(frozenStatusReadAdmission) {
		t.Fatalf("StatusReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenStatusReadAdmission, len(frozenStatusReadAdmission), StatusReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenStatusReadAdmission[i] {
			t.Errorf("StatusReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenStatusReadAdmission[i], StatusReadAdmissionContractVersion)
		}
	}
}
