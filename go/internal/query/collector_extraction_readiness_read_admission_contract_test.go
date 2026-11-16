// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// frozenExtractionReadinessReadAdmission pins the exact method set behind
// the #9669 G2 admission contract for the collector extraction readiness
// reads (GET /api/v0/collector-extraction-readiness plus GET
// /api/v0/collector-extraction-readiness/{family}): the static policy
// catalog reads the handler issues — the full catalog slice and the
// per-family lookup. The entries re-declare the staying static signatures
// whole in this extraction-readiness-scoped shape instead of embedding a
// shared port, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter review
// before satellite consumption.
var frozenExtractionReadinessReadAdmission = []string{
	"Catalog func() []extraction.Readiness",
	"Lookup func(scope.CollectorKind) (extraction.Readiness, bool)",
}

// extractionReadinessReadAdmissionMethodSet reflects the live
// ExtractionReadinessReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func extractionReadinessReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestExtractionReadinessReadAdmissionContractIsVersioned pins the #9669 G2
// admission-contract version for the collector extraction readiness reads.
// The version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestExtractionReadinessReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ExtractionReadinessReadAdmissionContractVersion != "v0" {
		t.Errorf("ExtractionReadinessReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ExtractionReadinessReadAdmissionContractVersion, "v0")
	}
	got := extractionReadinessReadAdmissionMethodSet(t, (*ExtractionReadinessReadAdmission)(nil))
	if len(got) != len(frozenExtractionReadinessReadAdmission) {
		t.Fatalf("ExtractionReadinessReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenExtractionReadinessReadAdmission, len(frozenExtractionReadinessReadAdmission), ExtractionReadinessReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenExtractionReadinessReadAdmission[i] {
			t.Errorf("ExtractionReadinessReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenExtractionReadinessReadAdmission[i], ExtractionReadinessReadAdmissionContractVersion)
		}
	}
}

// TestExtractionReadinessReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the two re-declared static catalog reads, with graph,
// search, list, snapshot, derive, build, evaluate, or sort legs riding along
// refused. The frozen method-set contract test pins the exact names; this
// test pins the exclusions the arbiter ruling demands. This pin is its own
// over the static extraction policy catalog: citing MultiCloud drift,
// TerraformDrift, or IaC pins here is void.
func TestExtractionReadinessReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ExtractionReadinessReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("ExtractionReadinessReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
			"Derive", "Build", "Classify", "Schema", "Evaluate", "Sort",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ExtractionReadinessReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// extractionReadinessAdmissionCanned is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the two pinned reads over fixed
// canned rows and is compile-asserted against the pin, so the proof below
// runs through the admission shape, not the wider static package.
type extractionReadinessAdmissionCanned struct {
	rows  []extraction.Readiness
	calls []string
}

var _ ExtractionReadinessReadAdmission = (*extractionReadinessAdmissionCanned)(nil)

func (s *extractionReadinessAdmissionCanned) Catalog() []extraction.Readiness {
	s.calls = append(s.calls, "Catalog")
	return s.rows
}

func (s *extractionReadinessAdmissionCanned) Lookup(family scope.CollectorKind) (extraction.Readiness, bool) {
	s.calls = append(s.calls, "Lookup")
	for _, row := range s.rows {
		if row.Family == family {
			return row, true
		}
	}
	return extraction.Readiness{}, false
}

// TestExtractionReadinessReadAdmissionWireShapesServeTheRoute proves the
// frozen shape is sufficient: the two pinned reads answer through the
// pin-typed canned double, and the static adapter serves the same two reads
// over the real policy catalog, so the pin covers the reads the routes serve
// with no wider surface.
func TestExtractionReadinessReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin ExtractionReadinessReadAdmission = &extractionReadinessAdmissionCanned{rows: []extraction.Readiness{
		{Family: scope.CollectorGit, DisplayName: "Git"},
	}}
	rows := pin.Catalog()
	if len(rows) != 1 || rows[0].Family != scope.CollectorGit {
		t.Fatalf("Catalog through the pin = %+v, want the one canned row", rows)
	}
	row, ok := pin.Lookup(scope.CollectorGit)
	if !ok || row.DisplayName != "Git" {
		t.Fatalf("Lookup(git) through the pin = (%+v, %v), want the canned row, true", row, ok)
	}
	if _, ok := pin.Lookup(scope.CollectorKind("not-a-family")); ok {
		t.Error("Lookup of an untracked family through the pin = true, want false")
	}

	var live ExtractionReadinessReadAdmission = extractionReadinessStaticReader{}
	liveRows := live.Catalog()
	if len(liveRows) == 0 {
		t.Fatal("Catalog through the static adapter is empty: the pin must cover the real policy catalog")
	}
	if _, ok := live.Lookup(liveRows[0].Family); !ok {
		t.Fatalf("Lookup(%q) through the static adapter = false, want the catalog row", liveRows[0].Family)
	}
	if _, ok := live.Lookup(scope.CollectorKind("not-a-family")); ok {
		t.Error("Lookup of an untracked family through the static adapter = true, want false")
	}
}

// seededExtractionReadinessDrift is a deliberately widened copy of the pin:
// the two frozen reads plus the neighboring per-profile Evaluate the routes
// never call. It never ships; it proves the frozen-set comparison below can
// see a one-method widening.
type seededExtractionReadinessDrift interface {
	ExtractionReadinessReadAdmission
	Evaluate(extraction.Profile) extraction.Readiness
}

// TestExtractionReadinessReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a clean
// tree precisely because the guard fires on drift — a guard that cannot see
// the seeded method is no guard.
func TestExtractionReadinessReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := extractionReadinessReadAdmissionMethodSet(t, (*seededExtractionReadinessDrift)(nil))
	if reflect.DeepEqual(got, frozenExtractionReadinessReadAdmission) {
		t.Fatal("seeded widening (Evaluate) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenExtractionReadinessReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenExtractionReadinessReadAdmission)+1)
	}
}
