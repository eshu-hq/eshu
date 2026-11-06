// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenRecoveryAdmission pins the exact method set behind the versioned
// #8872 admission contract for the core recovery operations (refinalize and
// replay-failed) the admin endpoints admit through RecoveryService. A
// renamed, added, removed, or re-signed method fails here until that list is
// deliberately updated; bump RecoveryAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
var frozenRecoveryAdmission = []string{
	"Refinalize func(context.Context, recovery.RefinalizeFilter) (recovery.RefinalizeResult, error)",
	"ReplayFailed func(context.Context, recovery.ReplayFilter) (recovery.ReplayResult, error)",
}

// recoveryAdmissionMethodSet reflects the live RecoveryAdmission method set
// into sorted "name signature" strings for comparison with the frozen list.
func recoveryAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*RecoveryAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestRecoveryAdmissionContractIsVersioned pins the #8872 admission-contract
// version for the core recovery operations (POST /api/v0/admin/refinalize and
// POST /api/v0/admin/recover-generations admit through RecoveryService). The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the recovery shape fails this test until the version
// bumps in the same diff.
func TestRecoveryAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if RecoveryAdmissionContractVersion != "v0" {
		t.Errorf("RecoveryAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", RecoveryAdmissionContractVersion, "v0")
	}
	got := recoveryAdmissionMethodSet(t)
	if len(got) != len(frozenRecoveryAdmission) {
		t.Fatalf("RecoveryAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenRecoveryAdmission, len(frozenRecoveryAdmission), RecoveryAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenRecoveryAdmission[i] {
			t.Errorf("RecoveryAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenRecoveryAdmission[i], RecoveryAdmissionContractVersion)
		}
	}
}
