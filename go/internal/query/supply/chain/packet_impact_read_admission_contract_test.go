// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// frozenSupplyChainImpactPacketReadAdmission pins the exact method set
// behind the versioned #9669 W5 admission contract for the supply-chain
// impact packet read (GET
// /api/v0/investigations/supply-chain/impact/packet, per the
// supplemental arbiter ruling on eshu-hq/eshu-api#97). The two reducer
// reads the route exercises (the explanation row plus the readiness
// snapshot, both already riding SupplyChainReadAdmission v0) plus the
// shared selector closure the D6 pin explicitly excludes (repository
// resolution through MatchRepositories and the graph fallback). The
// entries re-declare the port signatures in this packet-scoped shape
// instead of embedding the shared or family ports — do not widen the
// D6 pin, do not pin GraphQuery/ContentStore wholesale, and do not
// cite the D6 pin alone as packet coverage. A renamed, added, removed,
// or re-signed method fails here until that list is deliberately
// updated; bump SupplyChainImpactPacketReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// W-wave consumer: W5f packet single (the impact packet route ONLY;
// the repository story single rides its own story pin and neither
// route may cite the other's pin). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// packet leg.
var frozenSupplyChainImpactPacketReadAdmission = []string{
	"ExplainSupplyChainImpact func(context.Context, packet.ExplanationFilter) (impact.ExplanationRow, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
	"ReadSupplyChainImpactReadiness func(context.Context, impact.ReadinessQuery) (packet.ReadinessSnapshot, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// supplyChainImpactPacketReadAdmissionGraphStub is the test-only graph
// satisfier behind the witness: it answers the two pinned graph reads
// behind the shared selector fallback.
type supplyChainImpactPacketReadAdmissionGraphStub struct{}

func (supplyChainImpactPacketReadAdmissionGraphStub) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (supplyChainImpactPacketReadAdmissionGraphStub) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// supplyChainImpactPacketReadAdmissionCatalogStub is the test-only
// catalog satisfier behind the witness: it answers the pinned
// repository-selector catalog read.
type supplyChainImpactPacketReadAdmissionCatalogStub struct{}

func (supplyChainImpactPacketReadAdmissionCatalogStub) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

// supplyChainImpactPacketReadAdmissionReducerStub is the test-only
// reducer satisfier behind the witness: it answers the two pinned
// reducer reads (explanation row plus readiness snapshot).
type supplyChainImpactPacketReadAdmissionReducerStub struct{}

func (supplyChainImpactPacketReadAdmissionReducerStub) ExplainSupplyChainImpact(context.Context, packet.ExplanationFilter) (impact.ExplanationRow, error) {
	return impact.ExplanationRow{}, nil
}

func (supplyChainImpactPacketReadAdmissionReducerStub) ReadSupplyChainImpactReadiness(context.Context, impact.ReadinessQuery) (packet.ReadinessSnapshot, error) {
	return packet.ReadinessSnapshot{}, nil
}

// supplyChainImpactPacketReadAdmissionWitness proves
// SupplyChainImpactPacketReadAdmission is satisfiable: the graph,
// catalog, and reducer reads compose jointly, so no single store
// carries all five methods. It composes test-only stubs rather than
// inventing a production satisfier — the seam stays unwired.
type supplyChainImpactPacketReadAdmissionWitness struct {
	supplyChainImpactPacketReadAdmissionGraphStub
	supplyChainImpactPacketReadAdmissionCatalogStub
	supplyChainImpactPacketReadAdmissionReducerStub
}

var _ SupplyChainImpactPacketReadAdmission = supplyChainImpactPacketReadAdmissionWitness{}

// supplyChainImpactPacketReadAdmissionResignedWitness carries a
// deliberately re-signed ReadSupplyChainImpactReadiness (an extra
// trailing parameter). It MUST NOT satisfy
// SupplyChainImpactPacketReadAdmission: the seeded rejection test below
// proves a signature drift on any pinned method breaks the pin instead
// of sliding through silently.
type supplyChainImpactPacketReadAdmissionResignedWitness struct {
	supplyChainImpactPacketReadAdmissionWitness
}

func (supplyChainImpactPacketReadAdmissionResignedWitness) ReadSupplyChainImpactReadiness(context.Context, impact.ReadinessQuery, int) (packet.ReadinessSnapshot, error) {
	return packet.ReadinessSnapshot{}, nil
}

// supplyChainImpactPacketReadAdmissionMethodSet reflects the live
// SupplyChainImpactPacketReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func supplyChainImpactPacketReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SupplyChainImpactPacketReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSupplyChainImpactPacketReadAdmissionContractIsVersioned pins the
// #9669 W5 admission-contract version for the impact packet read. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list is
// deliberately updated. Bump
// SupplyChainImpactPacketReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestSupplyChainImpactPacketReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SupplyChainImpactPacketReadAdmissionContractVersion != "v0" {
		t.Errorf("SupplyChainImpactPacketReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SupplyChainImpactPacketReadAdmissionContractVersion, "v0")
	}
	got := supplyChainImpactPacketReadAdmissionMethodSet(t)
	if len(got) != len(frozenSupplyChainImpactPacketReadAdmission) {
		t.Fatalf("SupplyChainImpactPacketReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSupplyChainImpactPacketReadAdmission, len(frozenSupplyChainImpactPacketReadAdmission), SupplyChainImpactPacketReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSupplyChainImpactPacketReadAdmission[i] {
			t.Errorf("SupplyChainImpactPacketReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSupplyChainImpactPacketReadAdmission[i], SupplyChainImpactPacketReadAdmissionContractVersion)
		}
	}
}

// TestSupplyChainImpactPacketReadAdmissionRejectsResignedMethod is the
// seeded RED half of the contract proof: a witness whose
// ReadSupplyChainImpactReadiness carries one extra parameter MUST NOT
// satisfy the pin. If a re-signed method ever slides through, this test
// fails and the frozen shape above can no longer be trusted to catch
// drift.
func TestSupplyChainImpactPacketReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*SupplyChainImpactPacketReadAdmission)(nil)).Elem()
	if reflect.TypeOf(supplyChainImpactPacketReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("supplyChainImpactPacketReadAdmissionResignedWitness implements SupplyChainImpactPacketReadAdmission: a re-signed ReadSupplyChainImpactReadiness must break the pin")
	}
	if !reflect.TypeOf(supplyChainImpactPacketReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("supplyChainImpactPacketReadAdmissionWitness does not implement SupplyChainImpactPacketReadAdmission: the minimal shape must satisfy the pin")
	}
}
