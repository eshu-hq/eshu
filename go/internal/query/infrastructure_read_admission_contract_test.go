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

// frozenInfraReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the infrastructure reads
// (GET /api/v0/cloud/resources, GET /api/v0/ecosystem/overview, GET
// /api/v0/infra/resources/count, GET
// /api/v0/infra/resources/inventory, POST
// /api/v0/ecosystem/graph-summary, POST /api/v0/infra/relationships,
// POST /api/v0/infra/resources/search, POST
// /api/v0/relationships/catalog, POST /api/v0/relationships/edges):
// the two bounded graph reads, the cloud-resource identity list, and
// the two cheap-summary aggregates. The entries re-declare the store
// signatures in this infra-scoped shape instead of embedding the
// shared ports, so no other family can cite this pin — embedding or
// aliasing GraphQuery, CloudResourceListStore, or
// InfraResourceAggregateStore here would be a blanket pin, which the
// arbiter ruling forbids. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// InfraReadAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
//
// W-wave consumer: W5 infra (all 9 oracle Infra rows).
var frozenInfraReadAdmission = []string{
	"CountInfraResources func(context.Context, query.InfraResourceAggregateFilter) (query.InfraResourceAggregateCount, error)",
	"InfraResourceInventory func(context.Context, query.InfraResourceAggregateFilter, query.InfraResourceInventoryDimension, int, int) ([]query.InfraResourceInventoryRow, query.InfraResourceAggregateSource, error)",
	"ListCloudResourceIdentities func(context.Context, query.CloudResourceListPageFilter) ([]query.CloudResourceListIdentity, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// infraReadAdmissionWitness proves InfraReadAdmission is satisfiable
// with the minimal shape: two bounded graph reads, one
// cloud-resource list, and two aggregates, no wider store surface.
type infraReadAdmissionWitness struct{}

func (infraReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (infraReadAdmissionWitness) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (infraReadAdmissionWitness) ListCloudResourceIdentities(context.Context, CloudResourceListPageFilter) ([]CloudResourceListIdentity, error) {
	return nil, nil
}

func (infraReadAdmissionWitness) CountInfraResources(context.Context, InfraResourceAggregateFilter) (InfraResourceAggregateCount, error) {
	return InfraResourceAggregateCount{}, nil
}

func (infraReadAdmissionWitness) InfraResourceInventory(context.Context, InfraResourceAggregateFilter, InfraResourceInventoryDimension, int, int) ([]InfraResourceInventoryRow, InfraResourceAggregateSource, error) {
	return nil, "", nil
}

var _ InfraReadAdmission = infraReadAdmissionWitness{}

// infraReadAdmissionMethodSet reflects the live InfraReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list. The iface parameter forces every caller —
// including the seeded-drift RED proof below — through this same
// guard comparison, so a passing suite proves the guard itself can
// see drift rather than proving a private copy can.
func infraReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestInfraReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the infra reads. The frozen method
// set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump InfraReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestInfraReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if InfraReadAdmissionContractVersion != "v0" {
		t.Errorf("InfraReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", InfraReadAdmissionContractVersion, "v0")
	}
	got := infraReadAdmissionMethodSet(t, (*InfraReadAdmission)(nil))
	if len(got) != len(frozenInfraReadAdmission) {
		t.Fatalf("InfraReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenInfraReadAdmission, len(frozenInfraReadAdmission), InfraReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenInfraReadAdmission[i] {
			t.Errorf("InfraReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenInfraReadAdmission[i], InfraReadAdmissionContractVersion)
		}
	}
}

// TestInfraReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the five re-declared infra reads, with no catalog,
// search, mutation, or ledger surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestInfraReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*InfraReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 5 {
		t.Fatalf("InfraReadAdmission has %d methods, want exactly 5: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Catalog", "Search", "Delete", "Create", "Write", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("InfraReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededInfraDrift is a deliberately widened copy of the pin: the five
// frozen infra reads plus a relationship-catalog snapshot the infra
// lane never issues. It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededInfraDrift interface {
	InfraReadAdmission
	SnapshotInfraCatalog(context.Context) ([]string, error)
}

// TestInfraReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestInfraReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := infraReadAdmissionMethodSet(t, (*seededInfraDrift)(nil))
	if reflect.DeepEqual(got, frozenInfraReadAdmission) {
		t.Fatal("seeded widening (SnapshotInfraCatalog) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenInfraReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenInfraReadAdmission)+1)
	}
}
