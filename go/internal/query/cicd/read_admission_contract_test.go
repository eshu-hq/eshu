// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenCICDReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the CI/CD
// run-correlation reads (GET /api/v0/ci-cd/run-correlations,
// run-correlations/count, run-correlations/inventory): the bounded
// run-correlation list plus the count and inventory aggregates. The
// entries re-declare the store signatures in this CI/CD-scoped shape
// instead of embedding the shared ports, so no other family can cite
// this pin — embedding or aliasing
// querycontract.CICDRunCorrelationStore or RunCorrelationAggregateStore
// here would be a blanket pin, which the arbiter ruling forbids. A
// renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump CICDReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// W-wave consumer: W5 CICD (all 3 oracle CICD rows).
var frozenCICDReadAdmission = []string{
	"CountRunCorrelations func(context.Context, cicd.RunCorrelationAggregateFilter) (cicd.RunCorrelationAggregateCount, error)",
	"ListCICDRunCorrelations func(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)",
	"RunCorrelationInventory func(context.Context, cicd.RunCorrelationAggregateFilter, cicd.RunCorrelationInventoryDimension, int, int) ([]cicd.RunCorrelationInventoryRow, error)",
}

// cicdReadAdmissionWitness proves CICDReadAdmission is satisfiable with
// the minimal shape: one bounded correlation list plus two
// aggregates, no wider store surface.
type cicdReadAdmissionWitness struct{}

func (cicdReadAdmissionWitness) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return nil, nil
}

func (cicdReadAdmissionWitness) CountRunCorrelations(context.Context, RunCorrelationAggregateFilter) (RunCorrelationAggregateCount, error) {
	return RunCorrelationAggregateCount{}, nil
}

func (cicdReadAdmissionWitness) RunCorrelationInventory(context.Context, RunCorrelationAggregateFilter, RunCorrelationInventoryDimension, int, int) ([]RunCorrelationInventoryRow, error) {
	return nil, nil
}

var _ CICDReadAdmission = cicdReadAdmissionWitness{}

// cicdReadAdmissionMethodSet reflects the live CICDReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list. The iface parameter forces every caller —
// including the seeded-drift RED proof below — through this same
// guard comparison, so a passing suite proves the guard itself can
// see drift rather than proving a private copy can.
func cicdReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCICDReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the CI/CD run-correlation reads. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list
// is deliberately updated. Bump CICDReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestCICDReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CICDReadAdmissionContractVersion != "v0" {
		t.Errorf("CICDReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CICDReadAdmissionContractVersion, "v0")
	}
	got := cicdReadAdmissionMethodSet(t, (*CICDReadAdmission)(nil))
	if len(got) != len(frozenCICDReadAdmission) {
		t.Fatalf("CICDReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCICDReadAdmission, len(frozenCICDReadAdmission), CICDReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCICDReadAdmission[i] {
			t.Errorf("CICDReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCICDReadAdmission[i], CICDReadAdmissionContractVersion)
		}
	}
}

// TestCICDReadAdmissionWireShapesAreSealed proves the seal on the pin:
// exactly the three re-declared correlation reads, with no graph,
// mutation, snapshot, or ledger surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestCICDReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CICDReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("CICDReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Graph", "Search", "Delete", "Write", "Snapshot", "Ledger", "Grant", "Ingest",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CICDReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCICDDrift is a deliberately widened copy of the pin: the three
// frozen correlation reads plus a run-ingest write the CI/CD lane
// never issues. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededCICDDrift interface {
	CICDReadAdmission
	RecordRunCorrelation(context.Context, string) error
}

// TestCICDReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestCICDReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := cicdReadAdmissionMethodSet(t, (*seededCICDDrift)(nil))
	if reflect.DeepEqual(got, frozenCICDReadAdmission) {
		t.Fatal("seeded widening (RecordRunCorrelation) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCICDReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCICDReadAdmission)+1)
	}
}
