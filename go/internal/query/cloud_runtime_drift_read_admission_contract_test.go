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

// frozenCloudRuntimeDriftReadAdmission pins the exact method set behind
// the versioned #9669 W8b admission contract for the CloudRuntimeDrift
// family reads: the bounded active-finding page and the total active
// count the two drift routes admit through. The entries re-declare the
// MultiCloudRuntimeDriftStore signatures in this CloudRuntimeDrift-scoped
// shape instead of embedding the store, so no other family can cite this
// pin — a blanket or shared-port pin is forbidden, and the widened shape
// needs arbiter review before satellite consumption.
//
// D-wave consumer: W8b tier-1 CloudRuntimeDrift pin, per the arbiter W8b
// ruling on #9669 (APPROVED-WITH-CONDITIONS). Only this family's
// two-method shape is approved; every other W8b family mints after its
// own method census lands.
var frozenCloudRuntimeDriftReadAdmission = []string{
	"CountActiveMultiCloudRuntimeDriftFindings func(context.Context, query.MultiCloudRuntimeDriftFilter) (int, error)",
	"ListActiveMultiCloudRuntimeDriftFindings func(context.Context, query.MultiCloudRuntimeDriftFilter) ([]query.MultiCloudRuntimeDriftFindingRow, error)",
}

// cloudRuntimeDriftReadAdmissionWitness proves the pin is satisfiable by
// the fixture-backed store the handler tests already run against: it
// composes the package's existing fake rather than inventing a new
// satisfier.
var _ CloudRuntimeDriftReadAdmission = fakeMultiCloudRuntimeDriftStore{}

// cloudRuntimeDriftReadAdmissionMethodSet reflects the live
// CloudRuntimeDriftReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func cloudRuntimeDriftReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCloudRuntimeDriftReadAdmissionContractIsVersioned pins the #9669
// W8b admission-contract version for the CloudRuntimeDrift reads. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the
// version bumps in the same diff.
func TestCloudRuntimeDriftReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CloudRuntimeDriftReadAdmissionContractVersion != "v0" {
		t.Errorf("CloudRuntimeDriftReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CloudRuntimeDriftReadAdmissionContractVersion, "v0")
	}
	got := cloudRuntimeDriftReadAdmissionMethodSet(t, (*CloudRuntimeDriftReadAdmission)(nil))
	if len(got) != len(frozenCloudRuntimeDriftReadAdmission) {
		t.Fatalf("CloudRuntimeDriftReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCloudRuntimeDriftReadAdmission, len(frozenCloudRuntimeDriftReadAdmission), CloudRuntimeDriftReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCloudRuntimeDriftReadAdmission[i] {
			t.Errorf("CloudRuntimeDriftReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCloudRuntimeDriftReadAdmission[i], CloudRuntimeDriftReadAdmissionContractVersion)
		}
	}
}

// TestCloudRuntimeDriftReadAdmissionWireShapesAreSealed proves the W8b
// seal on the pin: exactly the two re-declared drift reads, with any
// graph, content, snapshot, ledger, stored-summary, management, or
// single-row surface refused. The frozen method-set contract test pins
// the exact names; this test pins the exclusions the arbiter ruling
// demands.
func TestCloudRuntimeDriftReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CloudRuntimeDriftReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("CloudRuntimeDriftReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "GraphQuery", "ContentStore", "ContentReader",
			"Snapshot", "Ledger", "Summary", "Search", "Resolve",
			"Inventory", "Management", "Selector", "Reachability",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CloudRuntimeDriftReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCloudRuntimeDriftWidening is a deliberately widened copy of the
// pin: the two frozen reads plus a single-row graph read, which the
// census keeps OUT. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededCloudRuntimeDriftWidening interface {
	CloudRuntimeDriftReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestCloudRuntimeDriftReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening. It
// passes on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestCloudRuntimeDriftReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := cloudRuntimeDriftReadAdmissionMethodSet(t, (*seededCloudRuntimeDriftWidening)(nil))
	if reflect.DeepEqual(got, frozenCloudRuntimeDriftReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCloudRuntimeDriftReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCloudRuntimeDriftReadAdmission)+1)
	}
}
