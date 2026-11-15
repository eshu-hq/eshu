// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenComponentExtensionReadAdmission pins the exact method set behind the
// versioned #9669 admission contract for the component extension inventory
// read (GET /api/v0/component-extensions and GET
// /api/v0/component-extensions/{component_id}/diagnostics): exactly the
// installed-package readback the routes admit through. A renamed, added,
// removed, or re-signed method fails here until that list is deliberately
// updated; bump ComponentExtensionReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// C3 consumer: the C3 leaf, per the arbiter SUPPLEMENTAL-PIN-OWED ruling on
// eshu-hq/eshu-api#101. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of this
// pin precedes satellite termination of the component-extension leg.
//
// This pin is narrow by construction: it re-declares exactly the Readback
// signature in this component-extension-scoped shape instead of embedding
// component.Registry or aliasing a shared store, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden, and the widened shape
// needs arbiter review before satellite consumption.
var frozenComponentExtensionReadAdmission = []string{
	"Readback func(component.Policy) ([]component.RegistryReadbackComponent, error)",
}

// componentExtensionReadAdmissionMethodSet reflects the live
// ComponentExtensionReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func componentExtensionReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestComponentExtensionReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the component extension inventory read. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the frozen
// list is deliberately updated and the version bumps in the same diff.
func TestComponentExtensionReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ComponentExtensionReadAdmissionContractVersion != "v0" {
		t.Errorf("ComponentExtensionReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ComponentExtensionReadAdmissionContractVersion, "v0")
	}
	got := componentExtensionReadAdmissionMethodSet(t, (*ComponentExtensionReadAdmission)(nil))
	if len(got) != len(frozenComponentExtensionReadAdmission) {
		t.Fatalf("ComponentExtensionReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenComponentExtensionReadAdmission, len(frozenComponentExtensionReadAdmission), ComponentExtensionReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenComponentExtensionReadAdmission[i] {
			t.Errorf("ComponentExtensionReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenComponentExtensionReadAdmission[i], ComponentExtensionReadAdmissionContractVersion)
		}
	}
}

// TestComponentExtensionReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared Readback, with no mutation, install,
// activation, grant, or snapshot surface riding along. The frozen method-set
// contract test pins the exact name; this test pins the exclusions the
// arbiter ruling demands.
func TestComponentExtensionReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ComponentExtensionReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("ComponentExtensionReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "Readback" {
		t.Fatalf("ComponentExtensionReadAdmission sole method = %q, want %q", name, "Readback")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Install", "Enable", "Disable", "Uninstall", "Plan", "List", "Load", "Grants", "Append", "Snapshot",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ComponentExtensionReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededComponentExtensionDrift is a deliberately widened copy of the pin:
// the one frozen read plus a List the component-extension lane never issues
// through this port. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededComponentExtensionDrift interface {
	ComponentExtensionReadAdmission
	List() string
}

// TestComponentExtensionReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestComponentExtensionReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := componentExtensionReadAdmissionMethodSet(t, (*seededComponentExtensionDrift)(nil))
	if reflect.DeepEqual(got, frozenComponentExtensionReadAdmission) {
		t.Fatal("seeded widening (List) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenComponentExtensionReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenComponentExtensionReadAdmission)+1)
	}
}
