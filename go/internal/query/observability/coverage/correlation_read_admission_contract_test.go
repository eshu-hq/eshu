// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coverage

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenObservabilityCoverageCorrelationReadAdmission pins the exact method
// set behind the versioned #9669 W8b admission contract for the
// observability coverage correlation read (GET
// /api/v0/observability/coverage/correlations): the single bounded
// correlation read the route admits through
// ListObservabilityCoverageCorrelations. The entry re-declares the staying
// ObservabilityCorrelationStore signature in this coverage-scoped shape
// instead of embedding the shared port, so no other family can cite this pin
// — a blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption.
//
// Satellite consumer: the W8b-corr leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenObservabilityCoverageCorrelationReadAdmission = []string{
	"ListObservabilityCoverageCorrelations func(context.Context, coverage.CorrelationFilter) ([]coverage.CorrelationRow, error)",
}

// observabilityCoverageCorrelationReadAdmissionMethodSet reflects the live
// ObservabilityCoverageCorrelationReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func observabilityCoverageCorrelationReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestObservabilityCoverageCorrelationReadAdmissionContractIsVersioned pins
// the #9669 W8b admission-contract version for the observability coverage
// correlation read. The version value plus the frozen method set below are
// the enforcement: a breaking change to the admission shape fails this test
// until the version bumps in the same diff.
func TestObservabilityCoverageCorrelationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ObservabilityCoverageCorrelationReadAdmissionContractVersion != "v0" {
		t.Errorf("ObservabilityCoverageCorrelationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ObservabilityCoverageCorrelationReadAdmissionContractVersion, "v0")
	}
	got := observabilityCoverageCorrelationReadAdmissionMethodSet(t, (*ObservabilityCoverageCorrelationReadAdmission)(nil))
	if len(got) != len(frozenObservabilityCoverageCorrelationReadAdmission) {
		t.Fatalf("ObservabilityCoverageCorrelationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenObservabilityCoverageCorrelationReadAdmission, len(frozenObservabilityCoverageCorrelationReadAdmission), ObservabilityCoverageCorrelationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenObservabilityCoverageCorrelationReadAdmission[i] {
			t.Errorf("ObservabilityCoverageCorrelationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenObservabilityCoverageCorrelationReadAdmission[i], ObservabilityCoverageCorrelationReadAdmissionContractVersion)
		}
	}
}

// TestObservabilityCoverageCorrelationReadAdmissionWireShapesAreSealed proves
// the seal on the pin: exactly the one re-declared coverage correlation
// read, with every graph, search, list, content, or snapshot surface riding
// along refused. The frozen method-set contract test pins the exact name;
// this test pins the exclusions the arbiter ruling demands.
func TestObservabilityCoverageCorrelationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ObservabilityCoverageCorrelationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("ObservabilityCoverageCorrelationReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "ListObservabilityCoverageCorrelations" {
		t.Fatalf("ObservabilityCoverageCorrelationReadAdmission sole method = %q, want %q", name, "ListObservabilityCoverageCorrelations")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ObservabilityCoverageCorrelationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededObservabilityCoverageCorrelationDrift is a deliberately widened copy
// of the pin: the one frozen read plus a graph Run the coverage lane never
// issues. It never ships; it proves the frozen-set comparison below can see
// a one-method widening.
type seededObservabilityCoverageCorrelationDrift interface {
	ObservabilityCoverageCorrelationReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestObservabilityCoverageCorrelationReadAdmissionSeededDriftIsDetected is
// the RED half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening. It
// passes on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestObservabilityCoverageCorrelationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := observabilityCoverageCorrelationReadAdmissionMethodSet(t, (*seededObservabilityCoverageCorrelationDrift)(nil))
	if reflect.DeepEqual(got, frozenObservabilityCoverageCorrelationReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenObservabilityCoverageCorrelationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenObservabilityCoverageCorrelationReadAdmission)+1)
	}
}
