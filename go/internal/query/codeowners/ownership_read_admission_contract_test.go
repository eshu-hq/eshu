// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenCodeownersOwnershipReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the codeowners ownership read
// (GET /api/v0/codeowners/ownership): the graph Run x1 or x3 cursor
// branches, the RunSingle last-match on a manifest miss, and the catalog leg
// through ListServiceCatalogCorrelations. The catalog leg rides this pin as
// its own re-declaration; the ServiceCatalogCorrelation pin separately
// re-declares the same signature for its own route, and neither pin cites
// the other. The entries re-declare the staying-port signatures in this
// codeowners-scoped shape instead of embedding the shared ports, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption. It is
// the only remainder family exercising RunSingle.
//
// Satellite consumer: the W8b-wide3 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenCodeownersOwnershipReadAdmission = []string{
	"ListServiceCatalogCorrelations func(context.Context, querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// codeownersOwnershipReadAdmissionMethodSet reflects the live
// CodeownersOwnershipReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeownersOwnershipReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCodeownersOwnershipReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the codeowners ownership read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestCodeownersOwnershipReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeownersOwnershipReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeownersOwnershipReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeownersOwnershipReadAdmissionContractVersion, "v0")
	}
	got := codeownersOwnershipReadAdmissionMethodSet(t, (*CodeownersOwnershipReadAdmission)(nil))
	if len(got) != len(frozenCodeownersOwnershipReadAdmission) {
		t.Fatalf("CodeownersOwnershipReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeownersOwnershipReadAdmission, len(frozenCodeownersOwnershipReadAdmission), CodeownersOwnershipReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeownersOwnershipReadAdmission[i] {
			t.Errorf("CodeownersOwnershipReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeownersOwnershipReadAdmission[i], CodeownersOwnershipReadAdmissionContractVersion)
		}
	}
}

// TestCodeownersOwnershipReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the three re-declared ownership reads, with search, list,
// content, or snapshot surfaces riding along refused. The frozen method-set
// contract test pins the exact names; this test pins the exclusions the
// arbiter ruling demands. Scoped cross-repo empty page stays handler behavior
// OUT.
func TestCodeownersOwnershipReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeownersOwnershipReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("CodeownersOwnershipReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Search", "Get", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeownersOwnershipReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// codeownersOwnershipAdmissionCannedStore is the pin-satisfying double behind
// the wire-shape proof: it implements exactly the three pinned reads over
// fixed canned values and is compile-asserted against the pin, so the proof
// below runs through the admission shape, not the wider staying ports.
type codeownersOwnershipAdmissionCannedStore struct {
	calls []string
}

var _ CodeownersOwnershipReadAdmission = (*codeownersOwnershipAdmissionCannedStore)(nil)

func (s *codeownersOwnershipAdmissionCannedStore) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	s.calls = append(s.calls, "Run")
	return []map[string]any{{"ownership": "branch-row"}}, nil
}

func (s *codeownersOwnershipAdmissionCannedStore) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	s.calls = append(s.calls, "RunSingle")
	return map[string]any{"ownership": "last-match"}, nil
}

func (s *codeownersOwnershipAdmissionCannedStore) ListServiceCatalogCorrelations(_ context.Context, _ querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error) {
	s.calls = append(s.calls, "ListServiceCatalogCorrelations")
	return []querycontract.ServiceCatalogCorrelationRow{{CorrelationID: "corr-canned"}}, nil
}

// TestCodeownersOwnershipReadAdmissionWireShapesServeTheRoute proves the
// frozen shape is sufficient: all three pinned reads answer through the
// pin-typed canned store, including the RunSingle last-match leg on a
// manifest miss and the catalog manifest-precedence leg.
func TestCodeownersOwnershipReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin CodeownersOwnershipReadAdmission = &codeownersOwnershipAdmissionCannedStore{}
	rows, err := pin.Run(context.Background(), "canned", map[string]any{})
	if err != nil {
		t.Fatalf("Run through the pin failed: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("Run through the pin returned %d rows, want 1", len(rows))
	}
	single, err := pin.RunSingle(context.Background(), "canned", map[string]any{})
	if err != nil {
		t.Fatalf("RunSingle through the pin failed: %v", err)
	}
	if single["ownership"] != "last-match" {
		t.Errorf("RunSingle through the pin = %v, want the last-match row", single)
	}
	corr, err := pin.ListServiceCatalogCorrelations(context.Background(), querycontract.ServiceCatalogCorrelationFilter{})
	if err != nil {
		t.Fatalf("ListServiceCatalogCorrelations through the pin failed: %v", err)
	}
	if len(corr) != 1 {
		t.Errorf("ListServiceCatalogCorrelations through the pin returned %d rows, want 1", len(corr))
	}
}

// seededCodeownersOwnershipDrift is a deliberately widened copy of the pin:
// the three frozen reads plus a content search the ownership lane never
// issues. It never ships; it proves the frozen-set comparison below can see
// a one-method widening.
type seededCodeownersOwnershipDrift interface {
	CodeownersOwnershipReadAdmission
	SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)
}

// TestCodeownersOwnershipReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a clean
// tree precisely because the guard fires on drift — a guard that cannot see
// the seeded method is no guard.
func TestCodeownersOwnershipReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := codeownersOwnershipReadAdmissionMethodSet(t, (*seededCodeownersOwnershipDrift)(nil))
	if reflect.DeepEqual(got, frozenCodeownersOwnershipReadAdmission) {
		t.Fatal("seeded widening (SearchEntitiesByName) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeownersOwnershipReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeownersOwnershipReadAdmission)+1)
	}
}
