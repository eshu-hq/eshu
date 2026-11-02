// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"reflect"
	"sort"
	"testing"
)

// frozenReplayLedgerAdmission pins the exact method set behind the
// versioned #8872 admission contract for the shared replay idempotency
// ledger. A renamed, added, removed, or re-signed method fails here and
// the version below bumps in the same diff, so the eshu-api extraction
// boundary cannot drift silently.
var frozenReplayLedgerAdmission = []string{
	"ClaimReplayIdempotency func(context.Context, string, string, time.Time) (admin.ReplayIdempotencyClaim, error)",
	"CompleteReplayIdempotency func(context.Context, string, int, []string, time.Time) error",
}

// replayLedgerAdmissionMethodSet reflects the live ReplayLedgerAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func replayLedgerAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*ReplayLedgerAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestReplayLedgerAdmissionContractIsVersioned pins the #8872
// admission-contract version for the shared replay idempotency ledger
// (admin_replay_requests) the acknowledgement, replay, reopen, and
// recover-generations routes share. The version value plus the frozen
// method set below are the enforcement: a breaking change to the ledger
// shape fails this test until the version bumps in the same diff.
func TestReplayLedgerAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ReplayLedgerAdmissionContractVersion != "v0" {
		t.Errorf("ReplayLedgerAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ReplayLedgerAdmissionContractVersion, "v0")
	}
	got := replayLedgerAdmissionMethodSet(t)
	if len(got) != len(frozenReplayLedgerAdmission) {
		t.Fatalf("ReplayLedgerAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenReplayLedgerAdmission, len(frozenReplayLedgerAdmission), ReplayLedgerAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenReplayLedgerAdmission[i] {
			t.Errorf("ReplayLedgerAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenReplayLedgerAdmission[i], ReplayLedgerAdmissionContractVersion)
		}
	}
}
