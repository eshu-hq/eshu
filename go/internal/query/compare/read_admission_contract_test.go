// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"reflect"
	"sort"
	"testing"
)

// frozenCompareReadAdmission pins the exact method set behind the
// versioned #9669 Gap 04 admission contract for POST
// /api/v0/compare/environments: the two graph reads the route admits
// through (Run, RunSingle) and the two content reads its service
// evidence leg admits through (ListRepoFiles, GetFileContent). The
// entries re-declare the shared-port signatures in this
// Compare-scoped shape instead of embedding the shared ports, so no
// other family can cite this pin — embedding or aliasing
// querycontract.GraphQuery or service.EvidenceReader here would be a
// blanket pin, which the arbiter ruling forbids permanently. A
// renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump
// CompareReadAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: Compare gap-04 deliberate later pin (D6 follow-up,
// per the arbiter D-wave ruling minting rule and the row-7 Path B
// ruling gap-04 disposition).
var frozenCompareReadAdmission = []string{
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// compareReadAdmissionMethodSet reflects the live
// CompareReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func compareReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CompareReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCompareReadAdmissionContractIsVersioned pins the #9669 Gap 04
// admission-contract version for the Compare POST read. The version
// value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the
// version bumps in the same diff.
func TestCompareReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CompareReadAdmissionContractVersion != "v0" {
		t.Errorf("CompareReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CompareReadAdmissionContractVersion, "v0")
	}
	got := compareReadAdmissionMethodSet(t)
	if len(got) != len(frozenCompareReadAdmission) {
		t.Fatalf("CompareReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCompareReadAdmission, len(frozenCompareReadAdmission), CompareReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCompareReadAdmission[i] {
			t.Errorf("CompareReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCompareReadAdmission[i], CompareReadAdmissionContractVersion)
		}
	}
}
