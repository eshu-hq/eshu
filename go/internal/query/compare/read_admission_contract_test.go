// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"context"
	"reflect"
	"sort"
	"strings"
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

// compareReadAdmissionWitness proves CompareReadAdmission is
// satisfiable: the core-owned graph and content stores satisfy it
// jointly, composed by the consumer — no single store carries all
// four methods. It composes the package's existing fakes rather than
// inventing a new satisfier.
type compareReadAdmissionWitness struct {
	fakeCompareGraphReader
	fakeCompareEvidenceReader
}

var _ CompareReadAdmission = compareReadAdmissionWitness{}

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
// admission-contract version for the Compare POST read. The frozen
// method set below is the enforcement: an added, removed, renamed,
// or re-signed method fails this test until the frozen list is
// deliberately updated. Bump CompareReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
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

// TestCompareReadAdmissionWireShapesAreSealed proves the seal on the pin:
// exactly the four re-declared reads (two graph, two content), with no
// suppression, mutation, snapshot, or ledger surface riding along. The
// frozen method-set contract test pins the exact names; this test pins
// the exclusion the arbiter ruling demands.
func TestCompareReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CompareReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 4 {
		t.Fatalf("CompareReadAdmission has %d methods, want exactly 4: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Suppress", "Mutat", "Append", "Write", "Delete", "Create", "Update", "Snapshot", "Ledger", "Tenant", "Exec", "Graph"} {
			if strings.Contains(name, banned) {
				t.Errorf("CompareReadAdmission.%s contains %q: suppression, mutation, and snapshot legs stay out of this pin", name, banned)
			}
		}
	}
}

// seededCompareDrift is a deliberately widened copy of the pin: the four
// frozen reads plus a snapshot export the compare route never serves. It
// never ships; it proves the frozen-set comparison below can see a
// one-method widening.
type seededCompareDrift interface {
	CompareReadAdmission
	SnapshotCompareEnvironments(context.Context) ([]byte, error)
}

// TestCompareReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean
// tree precisely because the guard fires on drift — a guard that cannot
// see the seeded method is no guard.
func TestCompareReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*seededCompareDrift)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	if len(got) != len(frozenCompareReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCompareReadAdmission)+1)
	}
	for _, want := range frozenCompareReadAdmission {
		found := false
		for _, have := range got {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("seeded drift dropped frozen entry %q: the seed must only widen, never narrow", want)
		}
	}
}
