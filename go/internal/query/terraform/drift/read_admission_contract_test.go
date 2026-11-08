// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"context"
	"reflect"
	"sort"
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
// strings for comparison with the frozen list.
func terraformDriftReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*TerraformDriftReadAdmission)(nil)).Elem()
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
	got := terraformDriftReadAdmissionMethodSet(t)
	if len(got) != len(frozenTerraformDriftReadAdmission) {
		t.Fatalf("TerraformDriftReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenTerraformDriftReadAdmission, len(frozenTerraformDriftReadAdmission), TerraformDriftReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenTerraformDriftReadAdmission[i] {
			t.Errorf("TerraformDriftReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenTerraformDriftReadAdmission[i], TerraformDriftReadAdmissionContractVersion)
		}
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
