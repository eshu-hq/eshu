// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenSemanticEvidenceReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the two semantic evidence reads
// (GET /api/v0/semantic/code-hints and GET
// /api/v0/semantic/documentation-observations): the single evidence read
// both routes admit through SemanticEvidence, differing only in factKind.
// The entry re-declares the staying semanticEvidenceStore signature in this
// semantic-scoped shape instead of embedding the shared port, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption.
// Unexported filter and read-model types keep this pin in package query.
//
// Satellite consumer: the W8b-semantic leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenSemanticEvidenceReadAdmission = []string{
	"SemanticEvidence func(context.Context, query.semanticEvidenceFilter) (query.semanticEvidenceListReadModel, error)",
}

// semanticEvidenceReadAdmissionMethodSet reflects the live
// SemanticEvidenceReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func semanticEvidenceReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSemanticEvidenceReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the semantic evidence reads. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestSemanticEvidenceReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SemanticEvidenceReadAdmissionContractVersion != "v0" {
		t.Errorf("SemanticEvidenceReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SemanticEvidenceReadAdmissionContractVersion, "v0")
	}
	got := semanticEvidenceReadAdmissionMethodSet(t, (*SemanticEvidenceReadAdmission)(nil))
	if len(got) != len(frozenSemanticEvidenceReadAdmission) {
		t.Fatalf("SemanticEvidenceReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSemanticEvidenceReadAdmission, len(frozenSemanticEvidenceReadAdmission), SemanticEvidenceReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSemanticEvidenceReadAdmission[i] {
			t.Errorf("SemanticEvidenceReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSemanticEvidenceReadAdmission[i], SemanticEvidenceReadAdmissionContractVersion)
		}
	}
}

// TestSemanticEvidenceReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared semantic evidence read, with every graph,
// search, list, content, or snapshot surface riding along refused. The frozen
// method-set contract test pins the exact name; this test pins the exclusions
// the arbiter ruling demands.
func TestSemanticEvidenceReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SemanticEvidenceReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("SemanticEvidenceReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "SemanticEvidence" {
		t.Fatalf("SemanticEvidenceReadAdmission sole method = %q, want %q", name, "SemanticEvidence")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "Snapshot", "Ledger", "Summary", "List",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("SemanticEvidenceReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededSemanticEvidenceDrift is a deliberately widened copy of the pin: the
// one frozen read plus a graph Run the semantic lane never issues. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededSemanticEvidenceDrift interface {
	SemanticEvidenceReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestSemanticEvidenceReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestSemanticEvidenceReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := semanticEvidenceReadAdmissionMethodSet(t, (*seededSemanticEvidenceDrift)(nil))
	if reflect.DeepEqual(got, frozenSemanticEvidenceReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSemanticEvidenceReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSemanticEvidenceReadAdmission)+1)
	}
}
