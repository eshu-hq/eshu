// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// frozenVisualizationReadAdmission pins the exact method set behind the
// #9669 G2 admission contract for the visualization derive read (POST
// /api/v0/visualizations/derive): the three static packet builders the
// derive dispatch issues — one per derivable view. The entries re-declare
// the staying static signatures whole in this visualization-scoped shape
// instead of embedding a shared port, so no other family can cite this pin
// — a blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption.
var frozenVisualizationReadAdmission = []string{
	"BuildEvidenceCitationPacket func(evidence.EvidenceCitationResponse, *querycontract.TruthEnvelope) visualization.VisualizationPacket",
	"BuildIncidentContextPacket func(model.IncidentContextResponse, *querycontract.TruthEnvelope) visualization.VisualizationPacket",
	"BuildServiceStoryPacket func(map[string]interface {}, *querycontract.TruthEnvelope) visualization.VisualizationPacket",
}

// visualizationReadAdmissionMethodSet reflects the live
// VisualizationReadAdmission method set into sorted "name signature" strings
// for comparison with the frozen list.
func visualizationReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestVisualizationReadAdmissionContractIsVersioned pins the #9669 G2
// admission-contract version for the visualization derive read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestVisualizationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if VisualizationReadAdmissionContractVersion != "v0" {
		t.Errorf("VisualizationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", VisualizationReadAdmissionContractVersion, "v0")
	}
	got := visualizationReadAdmissionMethodSet(t, (*VisualizationReadAdmission)(nil))
	if len(got) != len(frozenVisualizationReadAdmission) {
		t.Fatalf("VisualizationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenVisualizationReadAdmission, len(frozenVisualizationReadAdmission), VisualizationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenVisualizationReadAdmission[i] {
			t.Errorf("VisualizationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenVisualizationReadAdmission[i], VisualizationReadAdmissionContractVersion)
		}
	}
}

// TestVisualizationReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the three re-declared packet builders, with graph, search,
// list, catalog, lookup, classify, schema, or map-decode legs riding along
// refused. The frozen method-set contract test pins the exact names; this
// test pins the exclusions the arbiter ruling demands. This pin is its own
// over the static visualization derive: citing MultiCloud drift,
// TerraformDrift, or IaC pins here is void.
func TestVisualizationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*VisualizationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("VisualizationReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
			"Catalog", "Lookup", "Classify", "Schema", "FromMap", "Load",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("VisualizationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// visualizationAdmissionCanned is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the three pinned builders over
// fixed canned packets and is compile-asserted against the pin, so the proof
// below runs through the admission shape, not the wider static package.
type visualizationAdmissionCanned struct {
	story    Packet
	citation Packet
	incident Packet
	calls    []string
}

var _ VisualizationReadAdmission = (*visualizationAdmissionCanned)(nil)

func (s *visualizationAdmissionCanned) BuildServiceStoryPacket(map[string]any, *querycontract.TruthEnvelope) Packet {
	s.calls = append(s.calls, "BuildServiceStoryPacket")
	return s.story
}

func (s *visualizationAdmissionCanned) BuildEvidenceCitationPacket(evidence.EvidenceCitationResponse, *querycontract.TruthEnvelope) Packet {
	s.calls = append(s.calls, "BuildEvidenceCitationPacket")
	return s.citation
}

func (s *visualizationAdmissionCanned) BuildIncidentContextPacket(model.IncidentContextResponse, *querycontract.TruthEnvelope) Packet {
	s.calls = append(s.calls, "BuildIncidentContextPacket")
	return s.incident
}

// TestVisualizationReadAdmissionWireShapesServeTheRoute proves the frozen
// shape is sufficient: the three pinned builders answer through the
// pin-typed canned double, and the static adapter derives the explicit
// fail-closed unsupported packet over empty sources without a graph or
// content read, so the pin covers the derive the route serves with no
// wider surface.
func TestVisualizationReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin VisualizationReadAdmission = &visualizationAdmissionCanned{
		story:    Packet{View: ViewServiceStory, Title: "story", Supported: true},
		citation: Packet{View: ViewEvidenceCitation, Title: "citation", Supported: true},
		incident: Packet{View: ViewIncidentContext, Title: "incident", Supported: true},
	}
	if got := pin.BuildServiceStoryPacket(map[string]any{}, nil); got.View != ViewServiceStory || !got.Supported {
		t.Fatalf("BuildServiceStoryPacket through the pin = %+v, want the canned story packet", got)
	}
	if got := pin.BuildEvidenceCitationPacket(evidence.EvidenceCitationResponse{}, nil); got.View != ViewEvidenceCitation || !got.Supported {
		t.Fatalf("BuildEvidenceCitationPacket through the pin = %+v, want the canned citation packet", got)
	}
	if got := pin.BuildIncidentContextPacket(model.IncidentContextResponse{}, nil); got.View != ViewIncidentContext || !got.Supported {
		t.Fatalf("BuildIncidentContextPacket through the pin = %+v, want the canned incident packet", got)
	}

	var live VisualizationReadAdmission = visualizationStaticDeriver{}
	for _, tc := range []struct {
		name string
		got  Packet
	}{
		{"BuildServiceStoryPacket", live.BuildServiceStoryPacket(map[string]any{}, nil)},
		{"BuildEvidenceCitationPacket", live.BuildEvidenceCitationPacket(evidence.EvidenceCitationResponse{}, nil)},
		{"BuildIncidentContextPacket", live.BuildIncidentContextPacket(model.IncidentContextResponse{}, nil)},
	} {
		if tc.got.Supported || tc.got.View != ViewUnsupported || len(tc.got.Limitations) == 0 {
			t.Errorf("%s through the static adapter over an empty source = (supported=%v, view=%q, limitations=%v), want the explicit unsupported packet", tc.name, tc.got.Supported, tc.got.View, tc.got.Limitations)
		}
	}
}

// seededVisualizationDrift is a deliberately widened copy of the pin: the
// three frozen builders plus the neighboring FromMap transport adapter the
// derive dispatch never calls. It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededVisualizationDrift interface {
	VisualizationReadAdmission
	BuildEvidenceCitationPacketFromMap(map[string]any, *querycontract.TruthEnvelope) Packet
}

// TestVisualizationReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs must
// report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestVisualizationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := visualizationReadAdmissionMethodSet(t, (*seededVisualizationDrift)(nil))
	if reflect.DeepEqual(got, frozenVisualizationReadAdmission) {
		t.Fatal("seeded widening (BuildEvidenceCitationPacketFromMap) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenVisualizationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenVisualizationReadAdmission)+1)
	}
}
