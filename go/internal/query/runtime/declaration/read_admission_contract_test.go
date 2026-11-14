// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenRuntimeDeclarationReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the runtime declaration query
// (POST /api/v0/runtime-declarations/query): the single bounded graph read
// the route admits through Run — page always, excluded-count only when the
// request compares, names only on a non-empty degradable page. RunSingle
// stays OUT by construction: the lane never issues a single-row graph read.
// LoadSection serves the repository-context section, not this route, and
// stays OUT. The entry re-declares the staying GraphQuery signature in this
// declaration-scoped shape instead of embedding the shared port, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption.
//
// Satellite consumer: the W8b-graph2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenRuntimeDeclarationReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// runtimeDeclarationReadAdmissionMethodSet reflects the live
// RuntimeDeclarationReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func runtimeDeclarationReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestRuntimeDeclarationReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the runtime declaration query. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestRuntimeDeclarationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if RuntimeDeclarationReadAdmissionContractVersion != "v0" {
		t.Errorf("RuntimeDeclarationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", RuntimeDeclarationReadAdmissionContractVersion, "v0")
	}
	got := runtimeDeclarationReadAdmissionMethodSet(t, (*RuntimeDeclarationReadAdmission)(nil))
	if len(got) != len(frozenRuntimeDeclarationReadAdmission) {
		t.Fatalf("RuntimeDeclarationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenRuntimeDeclarationReadAdmission, len(frozenRuntimeDeclarationReadAdmission), RuntimeDeclarationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenRuntimeDeclarationReadAdmission[i] {
			t.Errorf("RuntimeDeclarationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenRuntimeDeclarationReadAdmission[i], RuntimeDeclarationReadAdmissionContractVersion)
		}
	}
}

// TestRuntimeDeclarationReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared graph Run, with RunSingle and every
// search, list, content, load, or snapshot surface riding along refused. The
// frozen method-set contract test pins the exact name; this test pins the
// exclusions the arbiter ruling demands.
func TestRuntimeDeclarationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*RuntimeDeclarationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("RuntimeDeclarationReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "Run" {
		t.Fatalf("RuntimeDeclarationReadAdmission sole method = %q, want %q", name, "Run")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Get", "Match", "List", "Load", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("RuntimeDeclarationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededRuntimeDeclarationDrift is a deliberately widened copy of the pin:
// the one frozen read plus a RunSingle the declaration lane never issues. It
// never ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededRuntimeDeclarationDrift interface {
	RuntimeDeclarationReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestRuntimeDeclarationReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestRuntimeDeclarationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := runtimeDeclarationReadAdmissionMethodSet(t, (*seededRuntimeDeclarationDrift)(nil))
	if reflect.DeepEqual(got, frozenRuntimeDeclarationReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenRuntimeDeclarationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenRuntimeDeclarationReadAdmission)+1)
	}
}
