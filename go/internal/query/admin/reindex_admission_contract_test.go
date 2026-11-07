// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"sort"
	"testing"
)

// frozenReindexAdmission pins the exact method set behind the versioned
// #9669 D7 admission contract for the operator reindex mutation the admin
// reindex endpoint admits through its fleet and repository-scoped ports. A
// renamed, added, removed, or re-signed method fails here until that list
// is deliberately updated; bump ReindexAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the enforcement)
// so dest consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: D7 (admin four, per the arbiter D-wave ruling on #9669).
var frozenReindexAdmission = []string{
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
	"RequestReindex func(context.Context, string) (time.Time, error)",
	"RequestRepositoryReindex func(context.Context, []string) ([]runtime.RepositoryReindexRequest, error)",
}

// reindexAdmissionMethodSet reflects the live ReindexAdmission method set
// into sorted "name signature" strings for comparison with the frozen list.
func reindexAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*ReindexAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestReindexAdmissionContractIsVersioned pins the #9669 D7
// admission-contract version for the operator reindex mutation (POST
// /api/v0/admin/reindex admits through ReindexRequester fleet-wide and
// through RepositoryCatalogMatcher plus RepositoryReindexRequester for a
// repository-scoped request). The version value plus the frozen method set
// below are the enforcement: a breaking change to the admission shape fails
// this test until the version bumps in the same diff.
func TestReindexAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ReindexAdmissionContractVersion != "v0" {
		t.Errorf("ReindexAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ReindexAdmissionContractVersion, "v0")
	}
	got := reindexAdmissionMethodSet(t)
	if len(got) != len(frozenReindexAdmission) {
		t.Fatalf("ReindexAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenReindexAdmission, len(frozenReindexAdmission), ReindexAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenReindexAdmission[i] {
			t.Errorf("ReindexAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenReindexAdmission[i], ReindexAdmissionContractVersion)
		}
	}
}
