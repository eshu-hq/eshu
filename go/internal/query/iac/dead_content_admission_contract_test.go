// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"reflect"
	"sort"
	"testing"
)

// frozenIaCDeadContentAdmission pins the exact method set behind the
// versioned #9669 G4 supplemental admission contract for the
// dead-route content fallback behind POST /api/v0/iac/dead: the two
// catalog reads the route exercises (ListRepositories for the
// repo-name enrichment, MatchRepositories for the access-filtered
// selector resolution) plus the two file reads of the blank-file
// reload (ListRepoFiles, GetFileContent), re-declared here in this
// IaC-scoped shape instead of embedding the shared ContentStore
// port, so no other family can cite this pin. A renamed, added,
// removed, or re-signed method fails here until the list is
// deliberately updated; bump
// IaCDeadContentAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
var frozenIaCDeadContentAdmission = []string{
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"ListRepositories func(context.Context) ([]querycontract.RepositoryCatalogEntry, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
}

// iaCDeadContentAdmissionMethodSet reflects the live
// IaCDeadContentAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func iaCDeadContentAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*IaCDeadContentAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestIaCDeadContentAdmissionContractIsVersioned pins the #9669 G4
// supplemental admission-contract version for the dead-route content
// fallback. The frozen method set is the enforcement: an added,
// removed, renamed, or re-signed method fails this test until the
// frozen list is deliberately updated.
func TestIaCDeadContentAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if IaCDeadContentAdmissionContractVersion != "v0" {
		t.Errorf("IaCDeadContentAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", IaCDeadContentAdmissionContractVersion, "v0")
	}
	got := iaCDeadContentAdmissionMethodSet(t)
	if len(got) != len(frozenIaCDeadContentAdmission) {
		t.Fatalf("IaCDeadContentAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenIaCDeadContentAdmission, len(frozenIaCDeadContentAdmission), IaCDeadContentAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenIaCDeadContentAdmission[i] {
			t.Errorf("IaCDeadContentAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenIaCDeadContentAdmission[i], IaCDeadContentAdmissionContractVersion)
		}
	}
}
