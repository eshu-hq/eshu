// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenStatusExtraReadAdmission pins the exact method set behind the
// versioned #9669 D6 admission contract for the extra-port Status
// family reads (the bounded LiveActivity and retirement-summary reads;
// the governance Summary leg stays carried relay-disclosed per the
// row-7 Path B ruling and is not pinned here). A renamed, added,
// removed, or re-signed
// method fails here until that list is deliberately updated; bump
// StatusExtraReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669, gaps 03+04 follow-up).
var frozenStatusExtraReadAdmission = []string{
	"ReadLiveActivity func(context.Context, int, bool, []string, []string) ([]status.LiveActivityRow, bool, error)",
	"ReadRetirementSummary func(context.Context) (status.RetirementSummary, error)",
}

// statusExtraReadAdmissionMethodSet reflects the live
// StatusExtraReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func statusExtraReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*StatusExtraReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestStatusExtraReadAdmissionContractIsVersioned pins the #9669 D6
// admission-contract version for the extra-port Status family reads.
// The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this
// test until the version bumps in the same diff.
func TestStatusExtraReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if StatusExtraReadAdmissionContractVersion != "v0" {
		t.Errorf("StatusExtraReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", StatusExtraReadAdmissionContractVersion, "v0")
	}
	got := statusExtraReadAdmissionMethodSet(t)
	if len(got) != len(frozenStatusExtraReadAdmission) {
		t.Fatalf("StatusExtraReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenStatusExtraReadAdmission, len(frozenStatusExtraReadAdmission), StatusExtraReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenStatusExtraReadAdmission[i] {
			t.Errorf("StatusExtraReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenStatusExtraReadAdmission[i], StatusExtraReadAdmissionContractVersion)
		}
	}
}
