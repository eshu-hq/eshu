// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capability

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// frozenCapabilityReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the capability catalog
// read (GET /api/v0/capabilities): the single embedded catalog load.
// The entry re-declares the staying static signature whole in this
// capability-scoped shape instead of aliasing the catalog load, so no
// other family can cite this pin — a blanket pin is forbidden, and
// the widened shape needs arbiter review before satellite
// consumption. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// CapabilityReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 capabilities (the single oracle Capabilities
// row).
var frozenCapabilityReadAdmission = []string{
	"LoadCapabilityCatalog func() (capabilitycatalog.Catalog, error)",
}

// capabilityReadAdmissionWitness proves CapabilityReadAdmission is
// satisfiable with the minimal shape: the single catalog load, no
// wider catalog surface.
type capabilityReadAdmissionWitness struct{}

func (capabilityReadAdmissionWitness) LoadCapabilityCatalog() (capabilitycatalog.Catalog, error) {
	return capabilitycatalog.Catalog{}, nil
}

var _ CapabilityReadAdmission = capabilityReadAdmissionWitness{}

// capabilityReadAdmissionMethodSet reflects the live
// CapabilityReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func capabilityReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCapabilityReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the capability catalog read. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list
// is deliberately updated. Bump CapabilityReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestCapabilityReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CapabilityReadAdmissionContractVersion != "v0" {
		t.Errorf("CapabilityReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CapabilityReadAdmissionContractVersion, "v0")
	}
	got := capabilityReadAdmissionMethodSet(t, (*CapabilityReadAdmission)(nil))
	if len(got) != len(frozenCapabilityReadAdmission) {
		t.Fatalf("CapabilityReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCapabilityReadAdmission, len(frozenCapabilityReadAdmission), CapabilityReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCapabilityReadAdmission[i] {
			t.Errorf("CapabilityReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCapabilityReadAdmission[i], CapabilityReadAdmissionContractVersion)
		}
	}
}

// TestCapabilityReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared catalog read, with no matrix,
// budget-proof, mutation, or ledger surface riding along. The frozen
// method-set contract test pins the exact name; this test pins the
// exclusion the arbiter ruling demands.
func TestCapabilityReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CapabilityReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("CapabilityReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Matrix", "Budget", "Delete", "Write", "Snapshot", "Ledger", "Publish",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CapabilityReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCapabilityDrift is a deliberately widened copy of the pin:
// the one frozen catalog read plus a matrix load the capability lane
// never issues. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededCapabilityDrift interface {
	CapabilityReadAdmission
	LoadCapabilityMatrix(string) (capabilitycatalog.Matrix, error)
}

// TestCapabilityReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestCapabilityReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := capabilityReadAdmissionMethodSet(t, (*seededCapabilityDrift)(nil))
	if reflect.DeepEqual(got, frozenCapabilityReadAdmission) {
		t.Fatal("seeded widening (LoadCapabilityMatrix) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCapabilityReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCapabilityReadAdmission)+1)
	}
}
