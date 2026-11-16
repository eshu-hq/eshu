// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// frozenSurfaceInventoryReadAdmission pins the exact method set behind the
// #9669 G2 admission contract for the surface inventory read (GET
// /api/v0/surface-inventory): the single static load the handler issues.
// The entry re-declares the staying static signature whole in this
// surface-inventory-scoped shape instead of embedding a shared port, so no
// other family can cite this pin — a blanket or shared-port pin is
// forbidden, and the widened shape needs arbiter review before satellite
// consumption.
var frozenSurfaceInventoryReadAdmission = []string{
	"LoadSurfaceInventory func() (capabilitycatalog.SurfaceInventory, error)",
}

// surfaceInventoryReadAdmissionMethodSet reflects the live
// SurfaceInventoryReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func surfaceInventoryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestSurfaceInventoryReadAdmissionContractIsVersioned pins the #9669 G2
// admission-contract version for the surface inventory read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestSurfaceInventoryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SurfaceInventoryReadAdmissionContractVersion != "v0" {
		t.Errorf("SurfaceInventoryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SurfaceInventoryReadAdmissionContractVersion, "v0")
	}
	got := surfaceInventoryReadAdmissionMethodSet(t, (*SurfaceInventoryReadAdmission)(nil))
	if len(got) != len(frozenSurfaceInventoryReadAdmission) {
		t.Fatalf("SurfaceInventoryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSurfaceInventoryReadAdmission, len(frozenSurfaceInventoryReadAdmission), SurfaceInventoryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSurfaceInventoryReadAdmission[i] {
			t.Errorf("SurfaceInventoryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSurfaceInventoryReadAdmission[i], SurfaceInventoryReadAdmissionContractVersion)
		}
	}
}

// TestSurfaceInventoryReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared static load, with graph, search, list,
// catalog, derive, build, classify, schema, or raw-artifact legs riding
// along refused. The frozen method-set contract test pins the exact name;
// this test pins the exclusions the arbiter ruling demands. This pin is its
// own over the generated surface inventory: citing MultiCloud drift,
// TerraformDrift, or IaC pins here is void.
func TestSurfaceInventoryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SurfaceInventoryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("SurfaceInventoryReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
			"Catalog", "Lookup", "Derive", "Build", "Classify", "Schema", "Raw",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("SurfaceInventoryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// surfaceInventoryAdmissionCanned is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the one pinned read over a fixed
// canned inventory and is compile-asserted against the pin, so the proof
// below runs through the admission shape, not the wider static package.
type surfaceInventoryAdmissionCanned struct {
	inventory capabilitycatalog.SurfaceInventory
	calls     []string
}

var _ SurfaceInventoryReadAdmission = (*surfaceInventoryAdmissionCanned)(nil)

func (s *surfaceInventoryAdmissionCanned) LoadSurfaceInventory() (capabilitycatalog.SurfaceInventory, error) {
	s.calls = append(s.calls, "LoadSurfaceInventory")
	return s.inventory, nil
}

// TestSurfaceInventoryReadAdmissionWireShapesServeTheRoute proves the frozen
// shape is sufficient: the pinned load answers through the pin-typed canned
// double, and the static adapter serves the same load over the real
// embedded artifact, so the pin covers the read the route serves with no
// wider surface.
func TestSurfaceInventoryReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin SurfaceInventoryReadAdmission = &surfaceInventoryAdmissionCanned{inventory: capabilitycatalog.SurfaceInventory{
		Version: "v1",
		Surfaces: []capabilitycatalog.SurfaceRecord{
			{Category: capabilitycatalog.SurfaceCommand, Name: "eshu-api"},
		},
	}}
	inventory, err := pin.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("LoadSurfaceInventory through the pin failed: %v", err)
	}
	if inventory.Version != "v1" || len(inventory.Surfaces) != 1 || inventory.Surfaces[0].Name != "eshu-api" {
		t.Fatalf("LoadSurfaceInventory through the pin = %+v, want the one canned surface", inventory)
	}

	var live SurfaceInventoryReadAdmission = surfaceInventoryStaticReader{}
	liveInventory, err := live.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("LoadSurfaceInventory through the static adapter failed: %v", err)
	}
	if len(liveInventory.Surfaces) == 0 {
		t.Error("LoadSurfaceInventory through the static adapter serves zero surfaces: the pin must cover the real embedded inventory")
	}
}

// seededSurfaceInventoryDrift is a deliberately widened copy of the pin: the
// frozen load plus the neighboring RawSurfaceArtifact byte read the route
// never serves. It never ships; it proves the frozen-set comparison below
// can see a one-method widening.
type seededSurfaceInventoryDrift interface {
	SurfaceInventoryReadAdmission
	RawSurfaceArtifact() []byte
}

// TestSurfaceInventoryReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestSurfaceInventoryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := surfaceInventoryReadAdmissionMethodSet(t, (*seededSurfaceInventoryDrift)(nil))
	if reflect.DeepEqual(got, frozenSurfaceInventoryReadAdmission) {
		t.Fatal("seeded widening (RawSurfaceArtifact) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSurfaceInventoryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSurfaceInventoryReadAdmission)+1)
	}
}
