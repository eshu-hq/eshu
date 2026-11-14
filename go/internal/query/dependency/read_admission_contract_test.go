// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dependency

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenDependencyReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the dependency inventory read
// (GET /api/v0/dependencies): the single bounded graph read the route admits
// through Run, built by one statement builder for the forward and reverse
// legs. RunSingle stays OUT by construction: the lane never issues a
// single-row graph read. The entry re-declares the staying GraphQuery
// signature in this dependency-scoped shape instead of embedding the shared
// port, so no other family can cite this pin — a blanket or shared-port pin
// is forbidden, and the widened shape needs arbiter review before satellite
// consumption.
//
// Satellite consumer: the W8b-graph1 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Note: this route is the standing relay-leg benchmark in satellite
// latency suites; the leaf re-points those benchmarks, never deletes them.
var frozenDependencyReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// dependencyReadAdmissionMethodSet reflects the live DependencyReadAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func dependencyReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestDependencyReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the dependency inventory read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestDependencyReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if DependencyReadAdmissionContractVersion != "v0" {
		t.Errorf("DependencyReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", DependencyReadAdmissionContractVersion, "v0")
	}
	got := dependencyReadAdmissionMethodSet(t, (*DependencyReadAdmission)(nil))
	if len(got) != len(frozenDependencyReadAdmission) {
		t.Fatalf("DependencyReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenDependencyReadAdmission, len(frozenDependencyReadAdmission), DependencyReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenDependencyReadAdmission[i] {
			t.Errorf("DependencyReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenDependencyReadAdmission[i], DependencyReadAdmissionContractVersion)
		}
	}
}

// TestDependencyReadAdmissionWireShapesAreSealed proves the seal on the pin:
// exactly the one re-declared graph Run, with RunSingle and every search,
// list, content, or snapshot surface riding along refused. The frozen
// method-set contract test pins the exact name; this test pins the exclusions
// the arbiter ruling demands.
func TestDependencyReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*DependencyReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("DependencyReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "Run" {
		t.Fatalf("DependencyReadAdmission sole method = %q, want %q", name, "Run")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("DependencyReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededDependencyDrift is a deliberately widened copy of the pin: the one
// frozen read plus a RunSingle the dependency lane never issues. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededDependencyDrift interface {
	DependencyReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestDependencyReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs must
// report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestDependencyReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := dependencyReadAdmissionMethodSet(t, (*seededDependencyDrift)(nil))
	if reflect.DeepEqual(got, frozenDependencyReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenDependencyReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenDependencyReadAdmission)+1)
	}
}
