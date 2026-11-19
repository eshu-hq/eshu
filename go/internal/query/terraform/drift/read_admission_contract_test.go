// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenTerraformDriftReadAdmission pins the exact method set behind the
// versioned #9669 O6 admission contract for POST
// /api/v0/terraform/config-state-drift/findings: the active-finding list
// and count reads the route admits through (ListActiveFindings,
// CountActiveFindings) with the FindingFilter double guard (Scoped plus
// AllowedScopeIDs). The entries re-declare the FindingStore signatures
// in this drift-scoped shape instead of embedding FindingStore, so no
// other family can cite this pin — embedding or aliasing FindingStore
// here would be a blanket pin, which the O6 ruling forbids. A renamed,
// added, removed, or re-signed method fails here until that list is
// deliberately updated; bump TerraformDriftReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: O6 pin 5 deliberate pin (per the O6 ruling minting
// order and the misc58 row-58 disposition).
var frozenTerraformDriftReadAdmission = []string{
	"CountActiveFindings func(context.Context, drift.FindingFilter) (int, error)",
	"ListActiveFindings func(context.Context, drift.FindingFilter) ([]drift.FindingRow, error)",
}

// terraformDriftReadAdmissionWitness proves TerraformDriftReadAdmission
// is satisfiable: the core-owned finding store satisfies it. It composes
// the package's existing fake rather than inventing a new satisfier.
var _ TerraformDriftReadAdmission = fakeTerraformConfigStateDriftStore{}

// terraformDriftReadAdmissionMethodSet reflects the live
// TerraformDriftReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func terraformDriftReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestTerraformDriftReadAdmissionContractIsVersioned pins the #9669 O6
// admission-contract version for the Terraform drift findings read. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list is
// deliberately updated. Bump TerraformDriftReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestTerraformDriftReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if TerraformDriftReadAdmissionContractVersion != "v0" {
		t.Errorf("TerraformDriftReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", TerraformDriftReadAdmissionContractVersion, "v0")
	}
	got := terraformDriftReadAdmissionMethodSet(t, (*TerraformDriftReadAdmission)(nil))
	if len(got) != len(frozenTerraformDriftReadAdmission) {
		t.Fatalf("TerraformDriftReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenTerraformDriftReadAdmission, len(frozenTerraformDriftReadAdmission), TerraformDriftReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenTerraformDriftReadAdmission[i] {
			t.Errorf("TerraformDriftReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenTerraformDriftReadAdmission[i], TerraformDriftReadAdmissionContractVersion)
		}
	}
}

// TestTerraformDriftReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the two re-declared finding reads, with no
// mutation, resolve, or snapshot surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestTerraformDriftReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*TerraformDriftReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("TerraformDriftReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Resolve", "Delete", "Create", "Write", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("TerraformDriftReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededTerraformDriftDrift is a deliberately widened copy of the pin:
// the two frozen finding reads plus an active-finding resolve, which
// the findings lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededTerraformDriftDrift interface {
	TerraformDriftReadAdmission
	ResolveActiveFinding(context.Context, string) (FindingRow, error)
}

// TestTerraformDriftReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestTerraformDriftReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := terraformDriftReadAdmissionMethodSet(t, (*seededTerraformDriftDrift)(nil))
	if reflect.DeepEqual(got, frozenTerraformDriftReadAdmission) {
		t.Fatal("seeded widening (ResolveActiveFinding) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenTerraformDriftReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenTerraformDriftReadAdmission)+1)
	}
}

// TestTerraformDriftReadAdmissionSeededProof proves the pinned port
// carries the seeded read path end to end: rows seeded into the
// store-backed satisfier flow back through the TerraformDriftReadAdmission
// interface value (list and count), and the FindingFilter double guard
// (Scoped plus AllowedScopeIDs) passes through the port untouched.
func TestTerraformDriftReadAdmissionSeededProof(t *testing.T) {
	t.Parallel()

	seeded := []FindingRow{
		{FactID: "fact-1", ScopeID: "scope-a", Address: "aws_subnet.x", Outcome: "drifted"},
		{FactID: "fact-2", ScopeID: "scope-b", Address: "aws_vpc.y", Outcome: "drifted"},
	}
	var observed FindingFilter
	var admission TerraformDriftReadAdmission = fakeTerraformConfigStateDriftStore{
		observedFilter: &observed,
		rows:           seeded,
		total:          len(seeded),
	}

	filter := FindingFilter{
		Outcome:         "drifted",
		Scoped:          true,
		AllowedScopeIDs: []string{"scope-a", "scope-b"},
	}
	rows, err := admission.ListActiveFindings(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListActiveFindings through TerraformDriftReadAdmission = error %v, want nil", err)
	}
	if len(rows) != len(seeded) {
		t.Fatalf("ListActiveFindings through TerraformDriftReadAdmission = %d rows, want %d seeded", len(rows), len(seeded))
	}
	for i := range rows {
		if rows[i].FactID != seeded[i].FactID {
			t.Errorf("row %d FactID = %q, want seeded %q", i, rows[i].FactID, seeded[i].FactID)
		}
	}
	if !observed.Scoped || len(observed.AllowedScopeIDs) != 2 {
		t.Errorf("FindingFilter double guard did not pass through the port: observed = %+v, want Scoped with 2 AllowedScopeIDs", observed)
	}

	total, err := admission.CountActiveFindings(context.Background(), filter)
	if err != nil {
		t.Fatalf("CountActiveFindings through TerraformDriftReadAdmission = error %v, want nil", err)
	}
	if total != len(seeded) {
		t.Errorf("CountActiveFindings through TerraformDriftReadAdmission = %d, want %d seeded", total, len(seeded))
	}
}
