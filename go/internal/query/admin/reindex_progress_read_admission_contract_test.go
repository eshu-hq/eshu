// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenReindexProgressReadAdmission pins the exact method set behind
// the versioned #9669 Gap 02 admission contract for the reindex
// progress read (GET /api/v0/admin/reindex/progress via
// ReindexProgressReader). A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// ReindexProgressReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: D3 (admin reads, per the arbiter D-wave ruling on
// #9669: Decision 3 / census Q4 — admin reads go native only after the
// pin).
var frozenReindexProgressReadAdmission = []string{
	"FullReconcileState func(context.Context, string) (scope.FullReconcileState, error)",
	"GetReindexState func(context.Context, string) (runtime.ReindexRequest, error)",
	"RepositoryReindexRequestedAt func(context.Context, []string) (map[string]time.Time, error)",
}

// reindexProgressReadAdmissionMethodSet reflects the live
// ReindexProgressReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func reindexProgressReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*ReindexProgressReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestReindexProgressReadAdmissionContractIsVersioned pins the #9669
// Gap 02 admission-contract version for the reindex progress read. The
// version value plus the frozen method set below are the enforcement:
// a breaking change to the admission shape fails this test until the
// version bumps in the same diff.
func TestReindexProgressReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ReindexProgressReadAdmissionContractVersion != "v0" {
		t.Errorf("ReindexProgressReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ReindexProgressReadAdmissionContractVersion, "v0")
	}
	got := reindexProgressReadAdmissionMethodSet(t)
	if len(got) != len(frozenReindexProgressReadAdmission) {
		t.Fatalf("ReindexProgressReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenReindexProgressReadAdmission, len(frozenReindexProgressReadAdmission), ReindexProgressReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenReindexProgressReadAdmission[i] {
			t.Errorf("ReindexProgressReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenReindexProgressReadAdmission[i], ReindexProgressReadAdmissionContractVersion)
		}
	}
}
