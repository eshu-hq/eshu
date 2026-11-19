// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenPackageRegistryReadAdmission pins the exact method set behind
// the versioned #9669 W5 admission contract for the package-registry
// reads (GET /api/v0/package-registry/packages, versions,
// dependencies, correlations, dependency-chains,
// packages/count, packages/inventory): the single bounded graph read
// (RunSingle stays OUT by construction), the correlation read, and
// the two package aggregates. The entries re-declare the store
// signatures in this package-registry-scoped shape instead of
// embedding the shared ports, so no other family can cite this pin —
// embedding or aliasing querycontract.GraphQuery, CorrelationStore,
// or AggregateStore here would be a blanket pin, which the arbiter
// ruling forbids. A renamed, added, removed, or re-signed method
// fails here until that list is deliberately updated; bump
// PackageRegistryReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 package-registry (all 7 oracle PackageRegistry
// rows).
var frozenPackageRegistryReadAdmission = []string{
	"CountPackageRegistryPackages func(context.Context, registry.AggregateFilter) (registry.AggregateCount, error)",
	"ListPackageRegistryCorrelations func(context.Context, registry.CorrelationFilter) (registry.CorrelationPage, error)",
	"PackageRegistryPackageInventory func(context.Context, registry.AggregateFilter, registry.InventoryDimension, int, int) ([]registry.InventoryRow, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// packageRegistryReadAdmissionWitness proves
// PackageRegistryReadAdmission is satisfiable with the minimal shape:
// one bounded graph read, one correlation read, and two aggregates,
// no wider store surface.
type packageRegistryReadAdmissionWitness struct{}

func (packageRegistryReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (packageRegistryReadAdmissionWitness) ListPackageRegistryCorrelations(context.Context, CorrelationFilter) (CorrelationPage, error) {
	return CorrelationPage{}, nil
}

func (packageRegistryReadAdmissionWitness) CountPackageRegistryPackages(context.Context, AggregateFilter) (AggregateCount, error) {
	return AggregateCount{}, nil
}

func (packageRegistryReadAdmissionWitness) PackageRegistryPackageInventory(context.Context, AggregateFilter, InventoryDimension, int, int) ([]InventoryRow, error) {
	return nil, nil
}

var _ PackageRegistryReadAdmission = packageRegistryReadAdmissionWitness{}

// packageRegistryReadAdmissionMethodSet reflects the live
// PackageRegistryReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list. The iface
// parameter forces every caller — including the seeded-drift RED
// proof below — through this same guard comparison, so a passing
// suite proves the guard itself can see drift rather than proving a
// private copy can.
func packageRegistryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestPackageRegistryReadAdmissionContractIsVersioned pins the #9669
// W5 admission-contract version for the package-registry reads. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list
// is deliberately updated. Bump
// PackageRegistryReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestPackageRegistryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if PackageRegistryReadAdmissionContractVersion != "v0" {
		t.Errorf("PackageRegistryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", PackageRegistryReadAdmissionContractVersion, "v0")
	}
	got := packageRegistryReadAdmissionMethodSet(t, (*PackageRegistryReadAdmission)(nil))
	if len(got) != len(frozenPackageRegistryReadAdmission) {
		t.Fatalf("PackageRegistryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenPackageRegistryReadAdmission, len(frozenPackageRegistryReadAdmission), PackageRegistryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenPackageRegistryReadAdmission[i] {
			t.Errorf("PackageRegistryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenPackageRegistryReadAdmission[i], PackageRegistryReadAdmissionContractVersion)
		}
	}
}

// TestPackageRegistryReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the four re-declared registry reads, with no
// single-row graph, content, mutation, or ledger surface riding
// along. The frozen method-set contract test pins the exact names;
// this test pins the exclusion the arbiter ruling demands.
func TestPackageRegistryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*PackageRegistryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 4 {
		t.Fatalf("PackageRegistryReadAdmission has %d methods, want exactly 4: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Get", "Delete", "Write", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("PackageRegistryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededPackageRegistryDrift is a deliberately widened copy of the
// pin: the four frozen registry reads plus a single-row graph read,
// which the registry lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededPackageRegistryDrift interface {
	PackageRegistryReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestPackageRegistryReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening.
// It passes on a clean tree precisely because the guard fires on
// drift — a guard that cannot see the seeded method is no guard.
func TestPackageRegistryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := packageRegistryReadAdmissionMethodSet(t, (*seededPackageRegistryDrift)(nil))
	if reflect.DeepEqual(got, frozenPackageRegistryReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenPackageRegistryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenPackageRegistryReadAdmission)+1)
	}
}
