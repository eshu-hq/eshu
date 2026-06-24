// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVisualizationDeriveRouteBuildsServiceStoryPacket(t *testing.T) {
	t.Parallel()

	handler := mountVisualizationHandler()
	truth := freshTruth()
	env := visualizationHTTPEnvelope(t, handler, map[string]any{
		"view":            string(VisualizationViewServiceStory),
		"source_response": storyResponseWithUpstream([]string{"r2", "r1"}),
		"source_truth":    truth,
	})
	packet := visualizationEnvelopePacket(t, env)

	if packet.View != VisualizationViewServiceStory {
		t.Fatalf("packet view = %q, want %q", packet.View, VisualizationViewServiceStory)
	}
	if !packet.Supported {
		t.Fatalf("packet supported = false, want true; limitations=%v", packet.Limitations)
	}
	if packet.Truth == nil || packet.Truth.Level != truth.Level || packet.Truth.Basis != truth.Basis {
		t.Fatalf("packet truth = %+v, want source truth %+v", packet.Truth, truth)
	}
	if got, want := packet.Truth.Freshness.State, truth.Freshness.State; got != want {
		t.Fatalf("packet truth freshness = %q, want source freshness %q", got, want)
	}
	if env.Truth == nil {
		t.Fatal("envelope truth = nil, want visualization packet derivation truth")
	}
	if got, want := env.Truth.Capability, "visualization.packet_derivation"; got != want {
		t.Fatalf("envelope truth capability = %q, want %q", got, want)
	}
	if got, want := env.Truth.Level, TruthLevelDerived; got != want {
		t.Fatalf("envelope truth level = %q, want %q", got, want)
	}
	if got, want := env.Truth.Basis, TruthBasisHybrid; got != want {
		t.Fatalf("envelope truth basis = %q, want %q", got, want)
	}
	if got, want := env.Truth.Profile, ProfileLocalLightweight; got != want {
		t.Fatalf("envelope truth profile = %q, want %q", got, want)
	}
	for i := 1; i < len(packet.Nodes); i++ {
		if packet.Nodes[i-1].ID >= packet.Nodes[i].ID {
			t.Fatalf("nodes not deterministically ordered: %+v", packet.Nodes)
		}
	}
}

func TestVisualizationDeriveRouteSupportsEvidenceCitationAndIncidentContext(t *testing.T) {
	t.Parallel()

	handler := mountVisualizationHandler()
	testCases := []struct {
		name   string
		view   VisualizationView
		source any
	}{
		{
			name:   "evidence citation",
			view:   VisualizationViewEvidenceCitation,
			source: citationResponse([]string{"entity-2", "entity-1"}),
		},
		{
			name:   "incident context",
			view:   VisualizationViewIncidentContext,
			source: incidentResponse([]IncidentEvidenceSlot{IncidentSlotIncident, IncidentSlotService}),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := visualizationHTTPEnvelope(t, handler, map[string]any{
				"view":            string(tc.view),
				"source_response": tc.source,
				"source_truth":    freshTruth(),
			})
			packet := visualizationEnvelopePacket(t, env)
			if packet.View != tc.view {
				t.Fatalf("packet view = %q, want %q", packet.View, tc.view)
			}
			if !packet.Supported {
				t.Fatalf("packet supported = false, want true; limitations=%v", packet.Limitations)
			}
			if len(packet.Nodes) == 0 {
				t.Fatalf("packet nodes empty, want renderable nodes")
			}
		})
	}
}

func TestVisualizationDeriveRouteReturnsUnsupportedPacketForEmptyKnownView(t *testing.T) {
	t.Parallel()

	env := visualizationHTTPEnvelope(t, mountVisualizationHandler(), map[string]any{
		"view":            string(VisualizationViewServiceStory),
		"source_response": map[string]any{},
		"source_truth":    freshTruth(),
	})
	packet := visualizationEnvelopePacket(t, env)

	if packet.Supported {
		t.Fatalf("packet supported = true, want unsupported packet")
	}
	if packet.View != VisualizationViewUnsupported {
		t.Fatalf("packet view = %q, want %q", packet.View, VisualizationViewUnsupported)
	}
	if len(packet.Limitations) == 0 {
		t.Fatalf("limitations empty, want explicit unsupported reason")
	}
	if len(packet.RecommendedNextCalls) == 0 {
		t.Fatalf("recommended next calls empty, want bounded follow-up")
	}
}

func TestVisualizationDeriveRouteRejectsUnknownView(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(map[string]any{
		"view":            "unknown",
		"source_response": map[string]any{},
	})
	if err != nil {
		t.Fatalf("json.Marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/visualizations/derive", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mountVisualizationHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
	}
}

func TestOpenAPISpecIncludesVisualizationDeriveRoute(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/visualizations/derive")
	post := mustMapField(t, path, "post")
	if got, want := post["operationId"], "deriveVisualizationPacket"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	requestBody := mustMapField(t, post, "requestBody")
	content := mustMapField(t, requestBody, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	for _, field := range []string{"view", "source_response", "source_truth"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("request schema missing %s", field)
		}
	}
	responses := mustMapField(t, post, "responses")
	okResponse := mustMapField(t, responses, "200")
	okContent := mustMapField(t, okResponse, "content")
	okJSON := mustMapField(t, okContent, "application/json")
	okSchema := mustMapField(t, okJSON, "schema")
	okProperties := mustMapField(t, okSchema, "properties")
	if _, ok := okProperties["visualization_packet"]; !ok {
		t.Fatalf("response schema missing visualization_packet")
	}
}

func mountVisualizationHandler() http.Handler {
	mux := http.NewServeMux()
	router := &APIRouter{Visualization: &VisualizationHandler{}}
	router.Mount(mux)
	return mux
}

func visualizationHTTPEnvelope(t *testing.T, handler http.Handler, body any) *ResponseEnvelope {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/visualizations/derive", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusOK)
	}

	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v body=%s", err, rec.Body.String())
	}
	return &env
}

func visualizationEnvelopePacket(t *testing.T, env *ResponseEnvelope) VisualizationPacket {
	t.Helper()

	if env == nil {
		t.Fatal("envelope nil")
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", env.Data)
	}
	raw, ok := data["visualization_packet"]
	if !ok {
		t.Fatalf("envelope data missing visualization_packet: %#v", data)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json.Marshal packet: %v", err)
	}
	var packet VisualizationPacket
	if err := json.Unmarshal(encoded, &packet); err != nil {
		t.Fatalf("json.Unmarshal packet: %v", err)
	}
	return packet
}
