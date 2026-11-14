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

// frozenGraphEntityInventoryReadAdmission pins the exact method set behind
// the versioned #9669 W8b admission contract for the graph entity inventory
// read (GET /api/v0/graph/entities): the single bounded graph read the route
// admits through Run — the kind-counts read always, the kind-list read only
// with kind=, two statements through one method. RunSingle stays OUT by
// construction: the lane never issues a single-row graph read. The entry
// re-declares the staying GraphQuery signature in this graph-entity-scoped
// shape instead of embedding the shared port, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden, and the widened
// shape needs arbiter review before satellite consumption. The CodeGraph and
// CodeRelationships pins explicitly exclude this route; citing either is
// void.
//
// Satellite consumer: the W8b-graph1 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenGraphEntityInventoryReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// graphEntityInventoryReadAdmissionMethodSet reflects the live
// GraphEntityInventoryReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func graphEntityInventoryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestGraphEntityInventoryReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the graph entity inventory read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestGraphEntityInventoryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if GraphEntityInventoryReadAdmissionContractVersion != "v0" {
		t.Errorf("GraphEntityInventoryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", GraphEntityInventoryReadAdmissionContractVersion, "v0")
	}
	got := graphEntityInventoryReadAdmissionMethodSet(t, (*GraphEntityInventoryReadAdmission)(nil))
	if len(got) != len(frozenGraphEntityInventoryReadAdmission) {
		t.Fatalf("GraphEntityInventoryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenGraphEntityInventoryReadAdmission, len(frozenGraphEntityInventoryReadAdmission), GraphEntityInventoryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenGraphEntityInventoryReadAdmission[i] {
			t.Errorf("GraphEntityInventoryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenGraphEntityInventoryReadAdmission[i], GraphEntityInventoryReadAdmissionContractVersion)
		}
	}
}

// TestGraphEntityInventoryReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared graph Run, with RunSingle and every
// search, list, content, or snapshot surface riding along refused. The frozen
// method-set contract test pins the exact name; this test pins the exclusions
// the arbiter ruling demands.
func TestGraphEntityInventoryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*GraphEntityInventoryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("GraphEntityInventoryReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "Run" {
		t.Fatalf("GraphEntityInventoryReadAdmission sole method = %q, want %q", name, "Run")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("GraphEntityInventoryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededGraphEntityInventoryDrift is a deliberately widened copy of the pin:
// the one frozen read plus a RunSingle the graph-entity lane never issues.
// It never ships; it proves the frozen-set comparison below can see a
// one-method widening.
type seededGraphEntityInventoryDrift interface {
	GraphEntityInventoryReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestGraphEntityInventoryReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a clean
// tree precisely because the guard fires on drift — a guard that cannot see
// the seeded method is no guard.
func TestGraphEntityInventoryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := graphEntityInventoryReadAdmissionMethodSet(t, (*seededGraphEntityInventoryDrift)(nil))
	if reflect.DeepEqual(got, frozenGraphEntityInventoryReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenGraphEntityInventoryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenGraphEntityInventoryReadAdmission)+1)
	}
}
