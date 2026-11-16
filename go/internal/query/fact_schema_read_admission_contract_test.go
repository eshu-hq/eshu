// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// frozenFactSchemaReadAdmission pins the exact method set behind the #9669
// G2 admission contract for the fact-schema-version reads (GET
// /api/v0/fact-schema-versions plus GET
// /api/v0/fact-schema-versions/{fact_kind}): the static registry reads the
// handler issues — the full registry copy, the per-kind lookup, and the
// candidate classification. The entries re-declare the staying static
// signatures whole in this fact-schema-scoped shape instead of embedding a
// shared port, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter review
// before satellite consumption.
var frozenFactSchemaReadAdmission = []string{
	"ClassifySchemaVersion func(string, string) facts.Compatibility",
	"SchemaVersion func(string) (string, bool)",
	"SupportedSchemaVersions func() map[string]string",
}

// factSchemaReadAdmissionMethodSet reflects the live
// FactSchemaReadAdmission method set into sorted "name signature" strings
// for comparison with the frozen list.
func factSchemaReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestFactSchemaReadAdmissionContractIsVersioned pins the #9669 G2
// admission-contract version for the fact-schema-version reads. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestFactSchemaReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if FactSchemaReadAdmissionContractVersion != "v0" {
		t.Errorf("FactSchemaReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", FactSchemaReadAdmissionContractVersion, "v0")
	}
	got := factSchemaReadAdmissionMethodSet(t, (*FactSchemaReadAdmission)(nil))
	if len(got) != len(frozenFactSchemaReadAdmission) {
		t.Fatalf("FactSchemaReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenFactSchemaReadAdmission, len(frozenFactSchemaReadAdmission), FactSchemaReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenFactSchemaReadAdmission[i] {
			t.Errorf("FactSchemaReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenFactSchemaReadAdmission[i], FactSchemaReadAdmissionContractVersion)
		}
	}
}

// TestFactSchemaReadAdmissionWireShapesAreSealed proves the seal on the pin:
// exactly the three re-declared static registry reads, with graph, search,
// list, catalog, derive, build, or validation legs riding along refused. The
// frozen method-set contract test pins the exact names; this test pins the
// exclusions the arbiter ruling demands. This pin is its own over the core
// fact-schema-version registry: citing MultiCloud drift, TerraformDrift, or
// IaC pins here is void.
func TestFactSchemaReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*FactSchemaReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("FactSchemaReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
			"Catalog", "Lookup", "Derive", "Build", "Validate",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("FactSchemaReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// factSchemaAdmissionCanned is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the three pinned reads over a
// fixed canned registry and is compile-asserted against the pin, so the
// proof below runs through the admission shape, not the wider static
// package.
type factSchemaAdmissionCanned struct {
	registry map[string]string
	calls    []string
}

var _ FactSchemaReadAdmission = (*factSchemaAdmissionCanned)(nil)

func (s *factSchemaAdmissionCanned) SupportedSchemaVersions() map[string]string {
	s.calls = append(s.calls, "SupportedSchemaVersions")
	out := make(map[string]string, len(s.registry))
	for k, v := range s.registry {
		out[k] = v
	}
	return out
}

func (s *factSchemaAdmissionCanned) SchemaVersion(factKind string) (string, bool) {
	s.calls = append(s.calls, "SchemaVersion")
	v, ok := s.registry[factKind]
	return v, ok
}

func (s *factSchemaAdmissionCanned) ClassifySchemaVersion(factKind, candidate string) facts.Compatibility {
	s.calls = append(s.calls, "ClassifySchemaVersion")
	supported, ok := s.registry[factKind]
	if !ok {
		return facts.CompatibilityUnknownKind
	}
	if candidate == supported {
		return facts.CompatibilitySupported
	}
	return facts.CompatibilityUnsupportedMajor
}

// TestFactSchemaReadAdmissionWireShapesServeTheRoute proves the frozen shape
// is sufficient: the three pinned reads answer through the pin-typed canned
// double, and the static adapter serves the same three reads over the real
// core registry, so the pin covers the reads the routes serve with no wider
// surface.
func TestFactSchemaReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin FactSchemaReadAdmission = &factSchemaAdmissionCanned{registry: map[string]string{"git.commit": "1.2.3"}}
	registry := pin.SupportedSchemaVersions()
	if len(registry) != 1 || registry["git.commit"] != "1.2.3" {
		t.Fatalf("SupportedSchemaVersions through the pin = %v, want the one canned row", registry)
	}
	version, ok := pin.SchemaVersion("git.commit")
	if !ok || version != "1.2.3" {
		t.Fatalf("SchemaVersion through the pin = (%q, %v), want (1.2.3, true)", version, ok)
	}
	if _, ok := pin.SchemaVersion("not.a.kind"); ok {
		t.Error("SchemaVersion of an unknown kind through the pin = true, want false")
	}
	if got := pin.ClassifySchemaVersion("git.commit", "1.2.3"); got != facts.CompatibilitySupported {
		t.Errorf("ClassifySchemaVersion of the supported version through the pin = %q, want supported", got)
	}
	if got := pin.ClassifySchemaVersion("not.a.kind", "1.2.3"); got != facts.CompatibilityUnknownKind {
		t.Errorf("ClassifySchemaVersion of an unknown kind through the pin = %q, want unknown_kind", got)
	}

	var live FactSchemaReadAdmission = factSchemaStaticReader{}
	liveRegistry := live.SupportedSchemaVersions()
	if len(liveRegistry) == 0 {
		t.Fatal("SupportedSchemaVersions through the static adapter is empty: the pin must cover the real core registry")
	}
	for kind, supported := range liveRegistry {
		version, ok := live.SchemaVersion(kind)
		if !ok || version != supported {
			t.Fatalf("SchemaVersion(%q) through the static adapter = (%q, %v), want (%q, true)", kind, version, ok, supported)
		}
		if got := live.ClassifySchemaVersion(kind, supported); got != facts.CompatibilitySupported {
			t.Fatalf("ClassifySchemaVersion(%q, supported) through the static adapter = %q, want supported", kind, got)
		}
		break
	}
}

// seededFactSchemaDrift is a deliberately widened copy of the pin: the three
// frozen reads plus the neighboring facts.ValidateSchemaVersion validation
// the routes never call. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededFactSchemaDrift interface {
	FactSchemaReadAdmission
	ValidateSchemaVersion(string, string) error
}

// TestFactSchemaReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs must
// report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestFactSchemaReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := factSchemaReadAdmissionMethodSet(t, (*seededFactSchemaDrift)(nil))
	if reflect.DeepEqual(got, frozenFactSchemaReadAdmission) {
		t.Fatal("seeded widening (ValidateSchemaVersion) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenFactSchemaReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenFactSchemaReadAdmission)+1)
	}
}
