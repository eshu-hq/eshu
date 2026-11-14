// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenServiceCatalogCorrelationReadAdmission pins the exact method set
// behind the versioned #9669 W8b admission contract for the service catalog
// correlation read (GET /api/v0/service-catalog/correlations): the
// correlation read, the optional local-descriptor evidence read, and the
// selector reach via MatchRepositories with the graph argument nil — so only
// ContentStore.MatchRepositories runs, and only for a non-canonical
// repository_id. Run/RunSingle are OUT by construction: no GraphQuery method
// is exercised on this route. The entries re-declare the staying-port
// signatures in this service-scoped shape instead of embedding the shared
// ports, so no other family can cite this pin — a blanket or shared-port pin
// is forbidden, and the widened shape needs arbiter review before satellite
// consumption.
//
// Satellite consumer: the W8b-wide3 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenServiceCatalogCorrelationReadAdmission = []string{
	"ListServiceCatalogCorrelations func(context.Context, querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error)",
	"ListServiceCatalogLocalDescriptorEvidence func(context.Context, string, int) ([]querycontract.ServiceCatalogLocalDescriptorEvidenceRow, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
}

// serviceCatalogCorrelationReadAdmissionMethodSet reflects the live
// ServiceCatalogCorrelationReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func serviceCatalogCorrelationReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestServiceCatalogCorrelationReadAdmissionContractIsVersioned pins the
// #9669 W8b admission-contract version for the service catalog correlation
// read. The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this test until
// the version bumps in the same diff.
func TestServiceCatalogCorrelationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ServiceCatalogCorrelationReadAdmissionContractVersion != "v0" {
		t.Errorf("ServiceCatalogCorrelationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ServiceCatalogCorrelationReadAdmissionContractVersion, "v0")
	}
	got := serviceCatalogCorrelationReadAdmissionMethodSet(t, (*ServiceCatalogCorrelationReadAdmission)(nil))
	if len(got) != len(frozenServiceCatalogCorrelationReadAdmission) {
		t.Fatalf("ServiceCatalogCorrelationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenServiceCatalogCorrelationReadAdmission, len(frozenServiceCatalogCorrelationReadAdmission), ServiceCatalogCorrelationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenServiceCatalogCorrelationReadAdmission[i] {
			t.Errorf("ServiceCatalogCorrelationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenServiceCatalogCorrelationReadAdmission[i], ServiceCatalogCorrelationReadAdmissionContractVersion)
		}
	}
}

// TestServiceCatalogCorrelationReadAdmissionWireShapesAreSealed proves the
// seal on the pin: exactly the three re-declared service catalog reads, with
// graph, search, content, or snapshot surfaces riding along refused. The
// frozen method-set contract test pins the exact names; this test pins the
// exclusions the arbiter ruling demands. Run/RunSingle are OUT by
// construction: the selector resolves with the graph argument nil. Selector
// 404/400/500 mapping and missing-evidence shaping stay handler behavior
// OUT.
func TestServiceCatalogCorrelationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ServiceCatalogCorrelationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("ServiceCatalogCorrelationReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Content", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ServiceCatalogCorrelationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// serviceCatalogCorrelationAdmissionCannedStore is the pin-satisfying double
// behind the wire-shape proof: it implements exactly the three pinned reads
// over fixed canned values and is compile-asserted against the pin, so the
// proof below runs through the admission shape, not the wider staying ports.
type serviceCatalogCorrelationAdmissionCannedStore struct {
	calls []string
}

var _ ServiceCatalogCorrelationReadAdmission = (*serviceCatalogCorrelationAdmissionCannedStore)(nil)

func (s *serviceCatalogCorrelationAdmissionCannedStore) ListServiceCatalogCorrelations(_ context.Context, _ querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error) {
	s.calls = append(s.calls, "ListServiceCatalogCorrelations")
	return []querycontract.ServiceCatalogCorrelationRow{{CorrelationID: "corr-canned"}}, nil
}

func (s *serviceCatalogCorrelationAdmissionCannedStore) ListServiceCatalogLocalDescriptorEvidence(_ context.Context, _ string, _ int) ([]querycontract.ServiceCatalogLocalDescriptorEvidenceRow, error) {
	s.calls = append(s.calls, "ListServiceCatalogLocalDescriptorEvidence")
	return []querycontract.ServiceCatalogLocalDescriptorEvidenceRow{{}}, nil
}

func (s *serviceCatalogCorrelationAdmissionCannedStore) MatchRepositories(_ context.Context, _ string) ([]querycontract.RepositoryCatalogEntry, error) {
	s.calls = append(s.calls, "MatchRepositories")
	return []querycontract.RepositoryCatalogEntry{{ID: "repo-canned"}}, nil
}

// TestServiceCatalogCorrelationReadAdmissionWireShapesServeTheRoute proves
// the frozen shape is sufficient: all three pinned reads answer through the
// pin-typed canned store, including the non-canonical repository selector leg
// and the optional local-descriptor leg.
func TestServiceCatalogCorrelationReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin ServiceCatalogCorrelationReadAdmission = &serviceCatalogCorrelationAdmissionCannedStore{}
	rows, err := pin.ListServiceCatalogCorrelations(context.Background(), querycontract.ServiceCatalogCorrelationFilter{})
	if err != nil {
		t.Fatalf("ListServiceCatalogCorrelations through the pin failed: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("ListServiceCatalogCorrelations through the pin returned %d rows, want 1", len(rows))
	}
	entries, err := pin.MatchRepositories(context.Background(), "repo-canned")
	if err != nil {
		t.Fatalf("MatchRepositories through the pin failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("MatchRepositories through the pin returned %d entries, want 1", len(entries))
	}
	if _, err := pin.ListServiceCatalogLocalDescriptorEvidence(context.Background(), "repo-canned", 10); err != nil {
		t.Fatalf("ListServiceCatalogLocalDescriptorEvidence through the pin failed: %v", err)
	}
}

// seededServiceCatalogCorrelationDrift is a deliberately widened copy of the
// pin: the three frozen reads plus a graph Run the catalog lane never
// issues. It never ships; it proves the frozen-set comparison below can see
// a one-method widening.
type seededServiceCatalogCorrelationDrift interface {
	ServiceCatalogCorrelationReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestServiceCatalogCorrelationReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that cannot
// see the seeded method is no guard.
func TestServiceCatalogCorrelationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := serviceCatalogCorrelationReadAdmissionMethodSet(t, (*seededServiceCatalogCorrelationDrift)(nil))
	if reflect.DeepEqual(got, frozenServiceCatalogCorrelationReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenServiceCatalogCorrelationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenServiceCatalogCorrelationReadAdmission)+1)
	}
}
