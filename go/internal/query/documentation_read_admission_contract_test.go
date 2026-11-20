// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenDocumentationReadAdmission pins the exact method set behind
// the versioned #9669 W5 admission contract for the documentation
// reads (GET /api/v0/documentation/findings, facts,
// findings/{finding_id}/evidence-packet,
// evidence-packets/{packet_id}/freshness, findings/count,
// findings/inventory): the six read-model reads plus the two
// findings aggregates. The entries re-declare the store signatures
// in this documentation-scoped shape instead of embedding the shared
// stores, so no other family can cite this pin — embedding the
// read-model or aggregate store here would be a blanket pin, which
// the arbiter ruling forbids. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// DocumentationReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 documentation (all 6 oracle Documentation
// rows).
var frozenDocumentationReadAdmission = []string{
	"CountDocumentationFindings func(context.Context, query.DocumentationFindingAggregateFilter) (query.DocumentationFindingAggregateCount, error)",
	"DocumentationEvidencePacket func(context.Context, string) (querycontract.DocumentationEvidencePacketReadModel, error)",
	"DocumentationEvidencePacketFreshness func(context.Context, string, string) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error)",
	"DocumentationEvidencePacketFreshnessWithFilter func(context.Context, querycontract.DocumentationEvidencePacketFreshnessFilter) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error)",
	"DocumentationEvidencePacketWithFilter func(context.Context, querycontract.DocumentationEvidencePacketFilter) (querycontract.DocumentationEvidencePacketReadModel, error)",
	"DocumentationFacts func(context.Context, querycontract.DocumentationFactFilter) (querycontract.DocumentationFactListReadModel, error)",
	"DocumentationFindingInventory func(context.Context, query.DocumentationFindingAggregateFilter, query.DocumentationFindingInventoryDimension, int, int) ([]query.DocumentationFindingInventoryRow, error)",
	"DocumentationFindings func(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error)",
}

// documentationReadAdmissionWitness proves DocumentationReadAdmission
// is satisfiable with the minimal shape: six read-model reads plus
// two aggregates, no wider store surface.
type documentationReadAdmissionWitness struct{}

func (documentationReadAdmissionWitness) DocumentationFindings(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error) {
	return querycontract.DocumentationFindingListReadModel{}, nil
}

func (documentationReadAdmissionWitness) DocumentationFacts(context.Context, querycontract.DocumentationFactFilter) (querycontract.DocumentationFactListReadModel, error) {
	return querycontract.DocumentationFactListReadModel{}, nil
}

func (documentationReadAdmissionWitness) DocumentationEvidencePacket(context.Context, string) (querycontract.DocumentationEvidencePacketReadModel, error) {
	return querycontract.DocumentationEvidencePacketReadModel{}, nil
}

func (documentationReadAdmissionWitness) DocumentationEvidencePacketFreshness(context.Context, string, string) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error) {
	return querycontract.DocumentationEvidencePacketFreshnessReadModel{}, nil
}

func (documentationReadAdmissionWitness) DocumentationEvidencePacketWithFilter(context.Context, querycontract.DocumentationEvidencePacketFilter) (querycontract.DocumentationEvidencePacketReadModel, error) {
	return querycontract.DocumentationEvidencePacketReadModel{}, nil
}

func (documentationReadAdmissionWitness) DocumentationEvidencePacketFreshnessWithFilter(context.Context, querycontract.DocumentationEvidencePacketFreshnessFilter) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error) {
	return querycontract.DocumentationEvidencePacketFreshnessReadModel{}, nil
}

func (documentationReadAdmissionWitness) CountDocumentationFindings(context.Context, DocumentationFindingAggregateFilter) (DocumentationFindingAggregateCount, error) {
	return DocumentationFindingAggregateCount{}, nil
}

func (documentationReadAdmissionWitness) DocumentationFindingInventory(context.Context, DocumentationFindingAggregateFilter, DocumentationFindingInventoryDimension, int, int) ([]DocumentationFindingInventoryRow, error) {
	return nil, nil
}

var _ DocumentationReadAdmission = documentationReadAdmissionWitness{}

// documentationReadAdmissionMethodSet reflects the live
// DocumentationReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func documentationReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestDocumentationReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the documentation reads. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump
// DocumentationReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestDocumentationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if DocumentationReadAdmissionContractVersion != "v0" {
		t.Errorf("DocumentationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", DocumentationReadAdmissionContractVersion, "v0")
	}
	got := documentationReadAdmissionMethodSet(t, (*DocumentationReadAdmission)(nil))
	if len(got) != len(frozenDocumentationReadAdmission) {
		t.Fatalf("DocumentationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenDocumentationReadAdmission, len(frozenDocumentationReadAdmission), DocumentationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenDocumentationReadAdmission[i] {
			t.Errorf("DocumentationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenDocumentationReadAdmission[i], DocumentationReadAdmissionContractVersion)
		}
	}
}

// TestDocumentationReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the eight re-declared documentation reads, with no
// archive, mutation, snapshot, or ledger surface riding along. The
// frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands.
func TestDocumentationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*DocumentationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 8 {
		t.Fatalf("DocumentationReadAdmission has %d methods, want exactly 8: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Archive", "Delete", "Create", "Write", "Snapshot", "Ledger", "Purge",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("DocumentationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededDocumentationDrift is a deliberately widened copy of the pin:
// the eight frozen documentation reads plus an archive read the
// documentation lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededDocumentationDrift interface {
	DocumentationReadAdmission
	ListDocumentationArchive(context.Context) ([]string, error)
}

// TestDocumentationReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestDocumentationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := documentationReadAdmissionMethodSet(t, (*seededDocumentationDrift)(nil))
	if reflect.DeepEqual(got, frozenDocumentationReadAdmission) {
		t.Fatal("seeded widening (ListDocumentationArchive) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenDocumentationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenDocumentationReadAdmission)+1)
	}
}
