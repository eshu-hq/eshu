// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintelhttp

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// frozenServiceIntelHTTPReadAdmission pins the exact method set behind
// the versioned #9669 W5 admission contract for the service
// intelligence report read (GET
// /api/v0/services/{service_name}/intelligence-report): the
// service-story envelope build, the incident evidence read, the
// supply-chain inventory read, and the report compose. The entries
// re-declare the source signatures in this service-intelligence-scoped
// shape instead of embedding the entity Handler or the source
// interfaces, so no other family can cite this pin — a blanket pin is
// forbidden, and the widened shape needs arbiter review before
// satellite consumption. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// ServiceIntelHTTPReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 service intelligence (the single oracle
// ServiceIntelHTTP row).
var frozenServiceIntelHTTPReadAdmission = []string{
	"BuildServiceStoryEnvelope func(context.Context, service.WorkloadSelector, string) (map[string]interface {}, *querycontract.TruthEnvelope, int, *querycontract.ErrorEnvelope)",
	"ComposeServiceIntelReport func(serviceintel.ReportInput) serviceintel.Report",
	"IncidentRecordsForWorkload func(context.Context, string) ([]serviceintel.IncidentRecord, error)",
	"SupplyChainInventoryForWorkload func(context.Context, string) (map[string]interface {}, error)",
}

// serviceIntelHTTPReadAdmissionWitness proves
// ServiceIntelHTTPReadAdmission is satisfiable with the minimal shape:
// one envelope build, two evidence reads, and one compose, no wider
// source surface.
type serviceIntelHTTPReadAdmissionWitness struct{}

func (serviceIntelHTTPReadAdmissionWitness) BuildServiceStoryEnvelope(context.Context, service.WorkloadSelector, string) (map[string]any, *querycontract.TruthEnvelope, int, *querycontract.ErrorEnvelope) {
	return nil, nil, 0, nil
}

func (serviceIntelHTTPReadAdmissionWitness) IncidentRecordsForWorkload(context.Context, string) ([]serviceintel.IncidentRecord, error) {
	return nil, nil
}

func (serviceIntelHTTPReadAdmissionWitness) SupplyChainInventoryForWorkload(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func (serviceIntelHTTPReadAdmissionWitness) ComposeServiceIntelReport(serviceintel.ReportInput) serviceintel.Report {
	return serviceintel.Report{}
}

var _ ServiceIntelHTTPReadAdmission = serviceIntelHTTPReadAdmissionWitness{}

// serviceIntelHTTPReadAdmissionMethodSet reflects the live
// ServiceIntelHTTPReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list. The iface
// parameter forces every caller — including the seeded-drift RED proof
// below — through this same guard comparison, so a passing suite
// proves the guard itself can see drift rather than proving a private
// copy can.
func serviceIntelHTTPReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestServiceIntelHTTPReadAdmissionContractIsVersioned pins the #9669
// W5 admission-contract version for the service intelligence report
// read. The frozen method set below is the enforcement: an added,
// removed, renamed, or re-signed method fails this test until the
// frozen list is deliberately updated. Bump
// ServiceIntelHTTPReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestServiceIntelHTTPReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ServiceIntelHTTPReadAdmissionContractVersion != "v0" {
		t.Errorf("ServiceIntelHTTPReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ServiceIntelHTTPReadAdmissionContractVersion, "v0")
	}
	got := serviceIntelHTTPReadAdmissionMethodSet(t, (*ServiceIntelHTTPReadAdmission)(nil))
	if len(got) != len(frozenServiceIntelHTTPReadAdmission) {
		t.Fatalf("ServiceIntelHTTPReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenServiceIntelHTTPReadAdmission, len(frozenServiceIntelHTTPReadAdmission), ServiceIntelHTTPReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenServiceIntelHTTPReadAdmission[i] {
			t.Errorf("ServiceIntelHTTPReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenServiceIntelHTTPReadAdmission[i], ServiceIntelHTTPReadAdmissionContractVersion)
		}
	}
}

// TestServiceIntelHTTPReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the four re-declared report reads, with no
// story-handler, evidence-loader, mutation, or snapshot surface riding
// along. The frozen method-set contract test pins the exact names;
// this test pins the exclusion the arbiter ruling demands.
func TestServiceIntelHTTPReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ServiceIntelHTTPReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 4 {
		t.Fatalf("ServiceIntelHTTPReadAdmission has %d methods, want exactly 4: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Resolve", "Load", "Delete", "Write", "Snapshot", "Ledger", "Publish",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ServiceIntelHTTPReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededServiceIntelHTTPDrift is a deliberately widened copy of the
// pin: the four frozen report reads plus a catalog-resolve read the
// report lane never issues. It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededServiceIntelHTTPDrift interface {
	ServiceIntelHTTPReadAdmission
	ResolveCatalogService(string) error
}

// TestServiceIntelHTTPReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening.
// It passes on a clean tree precisely because the guard fires on
// drift — a guard that cannot see the seeded method is no guard.
func TestServiceIntelHTTPReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := serviceIntelHTTPReadAdmissionMethodSet(t, (*seededServiceIntelHTTPDrift)(nil))
	if reflect.DeepEqual(got, frozenServiceIntelHTTPReadAdmission) {
		t.Fatal("seeded widening (ResolveCatalogService) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenServiceIntelHTTPReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenServiceIntelHTTPReadAdmission)+1)
	}
}
