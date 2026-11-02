// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"reflect"
	"sort"
	"testing"
)

// frozenTerminalFailureAdmission pins the exact method set behind the
// versioned #8872 admission contract. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump TerminalFailureAdmissionContractVersion alongside so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
var frozenTerminalFailureAdmission = []string{
	"Acknowledge func(context.Context, postgres.TerminalFailureAckRequest) (postgres.TerminalFailureAckResult, error)",
	"ListTerminalFailureAcks func(context.Context, postgres.TerminalFailureAckListFilter) ([]postgres.TerminalFailureAckRecord, error)",
	"ListTerminalFailures func(context.Context, postgres.TerminalFailureListFilter) (postgres.TerminalFailureListPage, error)",
	"ResolveTarget func(context.Context, postgres.TerminalFailureAckRequest) (postgres.TerminalFailureAckTarget, error)",
}

// admissionMethodSet reflects the live TerminalFailureAdmission method set
// into sorted "name signature" strings for comparison with the frozen list.
func admissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*TerminalFailureAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestTerminalFailureAdmissionContractIsVersioned pins the #8872
// admission-contract version for the operator terminal-failure
// acknowledgement mutation. The version value plus the frozen method set
// below are the enforcement: a breaking change to the admission shape fails
// this test until the version bumps in the same diff.
func TestTerminalFailureAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if TerminalFailureAdmissionContractVersion != "v0" {
		t.Errorf("TerminalFailureAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", TerminalFailureAdmissionContractVersion, "v0")
	}
	got := admissionMethodSet(t)
	if len(got) != len(frozenTerminalFailureAdmission) {
		t.Fatalf("TerminalFailureAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenTerminalFailureAdmission, len(frozenTerminalFailureAdmission), TerminalFailureAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenTerminalFailureAdmission[i] {
			t.Errorf("TerminalFailureAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenTerminalFailureAdmission[i], TerminalFailureAdmissionContractVersion)
		}
	}
}
