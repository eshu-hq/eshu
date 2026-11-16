// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenManifestDependenciesReadAdmission pins the exact method set behind
// the #9669 G2 admission contract for the repository manifest dependency
// read (GET /api/v0/repository/manifest-dependencies): the bounded
// store-backed page read the list path issues, lookahead bound and refused
// signal included. The entry re-declares the staying store signature whole
// in this manifest-dependencies-scoped shape instead of embedding the
// shared port, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter review
// before satellite consumption.
var frozenManifestDependenciesReadAdmission = []string{
	"ListRepositoryManifestDependencyFacts func(context.Context, string, *time.Time, string, int, bool) (manifest.ManifestDependencyPage, bool, error)",
}

// manifestDependenciesReadAdmissionMethodSet reflects the live
// ManifestDependenciesReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func manifestDependenciesReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestManifestDependenciesReadAdmissionContractIsVersioned pins the #9669 G2
// admission-contract version for the repository manifest dependency read.
// The version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestManifestDependenciesReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ManifestDependenciesReadAdmissionContractVersion != "v0" {
		t.Errorf("ManifestDependenciesReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ManifestDependenciesReadAdmissionContractVersion, "v0")
	}
	got := manifestDependenciesReadAdmissionMethodSet(t, (*ManifestDependenciesReadAdmission)(nil))
	if len(got) != len(frozenManifestDependenciesReadAdmission) {
		t.Fatalf("ManifestDependenciesReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenManifestDependenciesReadAdmission, len(frozenManifestDependenciesReadAdmission), ManifestDependenciesReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenManifestDependenciesReadAdmission[i] {
			t.Errorf("ManifestDependenciesReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenManifestDependenciesReadAdmission[i], ManifestDependenciesReadAdmissionContractVersion)
		}
	}
}

// TestManifestDependenciesReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared store-backed page read, with graph,
// search, catalog, derive, build, classify, schema, or context-section legs
// riding along refused. The frozen method-set contract test pins the exact
// name; this test pins the exclusions the arbiter ruling demands. This pin
// is its own over the manifest dependency store: citing MultiCloud drift,
// TerraformDrift, IaC, or repository-story pins here is void.
func TestManifestDependenciesReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ManifestDependenciesReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("ManifestDependenciesReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "Snapshot", "Ledger", "Summary",
			"Catalog", "Lookup", "Derive", "Build", "Classify", "Schema", "Section",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ManifestDependenciesReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// manifestDependenciesAdmissionCannedStore is the pin-satisfying double
// behind the wire-shape proof: it implements exactly the one pinned read
// over a fixed canned page and is compile-asserted against the pin, so the
// proof below runs through the admission shape, not the wider staying port.
type manifestDependenciesAdmissionCannedStore struct {
	page    ManifestDependencyPage
	refused bool
	calls   []string
}

var _ ManifestDependenciesReadAdmission = (*manifestDependenciesAdmissionCannedStore)(nil)

func (s *manifestDependenciesAdmissionCannedStore) ListRepositoryManifestDependencyFacts(_ context.Context, _ string, _ *time.Time, _ string, _ int, _ bool) (ManifestDependencyPage, bool, error) {
	s.calls = append(s.calls, "ListRepositoryManifestDependencyFacts")
	return s.page, s.refused, nil
}

// TestManifestDependenciesReadAdmissionWireShapesServeTheRoute proves the
// frozen shape is sufficient: the pinned page read answers through the
// pin-typed canned store, including the refused delta-base signal the list
// path maps to its fixed unavailable message, so the pin covers the read
// the route serves with no wider surface.
func TestManifestDependenciesReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin ManifestDependenciesReadAdmission = &manifestDependenciesAdmissionCannedStore{page: ManifestDependencyPage{
		Rows: []ManifestDependencyRow{
			{PackageName: "lodash", DeclaredRange: "^4.17.21", ManifestFile: "package.json", Ecosystem: "npm", PackageManager: "npm", Scope: "runtime", FactID: "fact-1"},
		},
		Freshness: ManifestDependencyFreshness{ScopeFound: true, FreshnessState: string(querycontract.FreshnessFresh)},
	}}
	page, refused, err := pin.ListRepositoryManifestDependencyFacts(context.Background(), "repo-1", nil, "", 51, false)
	if err != nil {
		t.Fatalf("ListRepositoryManifestDependencyFacts through the pin failed: %v", err)
	}
	if refused {
		t.Error("ListRepositoryManifestDependencyFacts through the pin reports refused, want the servable canned page")
	}
	if len(page.Rows) != 1 || page.Rows[0].PackageName != "lodash" {
		t.Fatalf("ListRepositoryManifestDependencyFacts through the pin = %+v, want the one canned row", page.Rows)
	}

	var refusedPin ManifestDependenciesReadAdmission = &manifestDependenciesAdmissionCannedStore{refused: true}
	if _, refused, err := refusedPin.ListRepositoryManifestDependencyFacts(context.Background(), "repo-1", nil, "", 51, false); err != nil || !refused {
		t.Fatalf("ListRepositoryManifestDependencyFacts through the refused pin = (refused=%v, err=%v), want (true, nil): the refused signal must be servable through the pin", refused, err)
	}
}

// seededManifestDependenciesDrift is a deliberately widened copy of the pin:
// the frozen page read plus a graph Run the manifest lane never issues. It
// never ships; it proves the frozen-set comparison below can see a
// one-method widening.
type seededManifestDependenciesDrift interface {
	ManifestDependenciesReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestManifestDependenciesReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a clean
// tree precisely because the guard fires on drift — a guard that cannot see
// the seeded method is no guard.
func TestManifestDependenciesReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := manifestDependenciesReadAdmissionMethodSet(t, (*seededManifestDependenciesDrift)(nil))
	if reflect.DeepEqual(got, frozenManifestDependenciesReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenManifestDependenciesReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenManifestDependenciesReadAdmission)+1)
	}
}
