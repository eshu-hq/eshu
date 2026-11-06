// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenAdminReadAdmission pins the exact method set behind the
// versioned #9669 D1 admission contract for the operator admin reads
// the admin endpoints admit through ReadStore. A renamed, added,
// removed, or re-signed method fails here until that list is
// deliberately updated; bump AdminReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: D3 (admin/identity/recovery reads, per the arbiter
// D-wave ruling on #9669).
var frozenAdminReadAdmission = []string{
	"ListChangedSincePoisonedLinks func(context.Context, admin.ChangedSincePoisonedLinkFilter) ([]admin.ChangedSincePoisonedLink, error)",
	"ListDeadLetterWorkItems func(context.Context, admin.DeadLetterListFilter) ([]admin.DeadLetterWorkItem, error)",
	"ListDecisions func(context.Context, admin.DecisionQueryFilter) ([]admin.DecisionRow, error)",
	"ListEvidence func(context.Context, string) ([]admin.EvidenceRow, error)",
	"ListReducerInputInvalidFacts func(context.Context, admin.InputInvalidFactListFilter) ([]admin.InputInvalidFact, error)",
	"ListReplayEvents func(context.Context, admin.ReplayEventFilter) ([]admin.ReplayEvent, error)",
	"ListWorkItems func(context.Context, admin.WorkItemFilter) ([]admin.WorkItem, error)",
}

// adminReadAdmissionMethodSet reflects the live AdminReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list.
func adminReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*AdminReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestAdminReadAdmissionContractIsVersioned pins the #9669 D1
// admission-contract version for the operator admin reads (the admin
// endpoints admit through ReadStore). The version value plus the frozen
// method set below are the enforcement: a breaking change to the
// admission shape fails this test until the version bumps in the same
// diff.
func TestAdminReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if AdminReadAdmissionContractVersion != "v0" {
		t.Errorf("AdminReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", AdminReadAdmissionContractVersion, "v0")
	}
	got := adminReadAdmissionMethodSet(t)
	if len(got) != len(frozenAdminReadAdmission) {
		t.Fatalf("AdminReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenAdminReadAdmission, len(frozenAdminReadAdmission), AdminReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenAdminReadAdmission[i] {
			t.Errorf("AdminReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenAdminReadAdmission[i], AdminReadAdmissionContractVersion)
		}
	}
}
