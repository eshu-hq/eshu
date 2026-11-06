// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"reflect"
	"sort"
	"testing"
)

// frozenTerminalFailureReadAdmission pins the exact method set behind
// the versioned #9669 D1 admission contract for the operator
// terminal-failure reads. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// TerminalFailureReadAdmissionContractVersion alongside so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// D-wave consumer: D3 (admin/identity/recovery reads, per the arbiter
// D-wave ruling on #9669).
var frozenTerminalFailureReadAdmission = []string{
	"ListTerminalFailureAcks func(context.Context, postgres.TerminalFailureAckListFilter) ([]postgres.TerminalFailureAckRecord, error)",
	"ListTerminalFailures func(context.Context, postgres.TerminalFailureListFilter) (postgres.TerminalFailureListPage, error)",
}

// terminalFailureReadAdmissionMethodSet reflects the live
// TerminalFailureReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func terminalFailureReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*TerminalFailureReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestTerminalFailureReadAdmissionContractIsVersioned pins the #9669 D1
// admission-contract version for the operator terminal-failure reads
// (the admin-only acknowledgement list and the per-instance failure
// discovery list). The version value plus the frozen method set below
// are the enforcement: a breaking change to the admission shape fails
// this test until the version bumps in the same diff.
func TestTerminalFailureReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if TerminalFailureReadAdmissionContractVersion != "v0" {
		t.Errorf("TerminalFailureReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", TerminalFailureReadAdmissionContractVersion, "v0")
	}
	got := terminalFailureReadAdmissionMethodSet(t)
	if len(got) != len(frozenTerminalFailureReadAdmission) {
		t.Fatalf("TerminalFailureReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenTerminalFailureReadAdmission, len(frozenTerminalFailureReadAdmission), TerminalFailureReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenTerminalFailureReadAdmission[i] {
			t.Errorf("TerminalFailureReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenTerminalFailureReadAdmission[i], TerminalFailureReadAdmissionContractVersion)
		}
	}
}
