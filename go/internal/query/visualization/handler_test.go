// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// These tests drive Handler through its own Mount, without the root
// APIRouter, so the leaf proves its route registration and request handling
// on its own. The root surface test keeps the APIRouter and OpenAPI wiring.

func TestHandlerDeriveMountsRouteAndBuildsServiceStoryPacket(t *testing.T) {
	t.Parallel()

	truth := querytestutil.FreshTruth()
	rec := postDerive(t, map[string]any{
		"view":            string(ViewServiceStory),
		"source_response": querytestutil.StoryResponseWithUpstream([]string{"r2", "r1"}),
		"source_truth":    truth,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusOK)
	}
	env, packet := decodeDeriveEnvelope(t, rec)

	if packet.View != ViewServiceStory {
		t.Fatalf("packet view = %q, want %q", packet.View, ViewServiceStory)
	}
	if !packet.Supported {
		t.Fatalf("packet supported = false, want true; limitations=%v", packet.Limitations)
	}
	if packet.Truth == nil || packet.Truth.Level != truth.Level || packet.Truth.Basis != truth.Basis {
		t.Fatalf("packet truth = %+v, want source truth %+v", packet.Truth, truth)
	}
	if env.Truth == nil {
		t.Fatal("envelope truth = nil, want visualization packet derivation truth")
	}
	if got, want := env.Truth.Capability, "visualization.packet_derivation"; got != want {
		t.Fatalf("envelope truth capability = %q, want %q", got, want)
	}
	if got, want := env.Truth.Level, querycontract.TruthLevelDerived; got != want {
		t.Fatalf("envelope truth level = %q, want %q", got, want)
	}
	if got, want := env.Truth.Basis, querycontract.TruthBasisHybrid; got != want {
		t.Fatalf("envelope truth basis = %q, want %q", got, want)
	}
	for i := 1; i < len(packet.Nodes); i++ {
		if packet.Nodes[i-1].ID >= packet.Nodes[i].ID {
			t.Fatalf("nodes not deterministically ordered: %+v", packet.Nodes)
		}
	}
}

func TestHandlerDeriveCarriesSourceProfileIntoDerivationTruth(t *testing.T) {
	t.Parallel()

	truth := querytestutil.FreshTruth()
	truth.Profile = querycontract.ProfileProduction
	rec := postDerive(t, map[string]any{
		"view":            string(ViewServiceStory),
		"source_response": querytestutil.StoryResponseWithUpstream([]string{"r1"}),
		"source_truth":    truth,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusOK)
	}
	env, _ := decodeDeriveEnvelope(t, rec)
	if env.Truth == nil {
		t.Fatal("envelope truth = nil, want derivation truth")
	}
	if got, want := env.Truth.Profile, querycontract.ProfileProduction; got != want {
		t.Fatalf("envelope truth profile = %q, want the source profile %q", got, want)
	}
}

func TestHandlerDeriveRejectsUnknownView(t *testing.T) {
	t.Parallel()

	rec := postDerive(t, map[string]any{
		"view":            "unknown",
		"source_response": map[string]any{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "view must be one of") {
		t.Fatalf("body = %s, want the unknown-view detail", rec.Body.String())
	}
}

func TestHandlerDeriveRejectsMalformedSourceResponse(t *testing.T) {
	t.Parallel()

	// A JSON array is valid JSON but cannot decode into the evidence-citation
	// read model, so the failure comes from the source decode, not ReadJSON.
	rec := postDerive(t, map[string]any{
		"view":            string(ViewEvidenceCitation),
		"source_response": []any{"not", "a", "citation", "response"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "invalid source_response") {
		t.Fatalf("body = %s, want the invalid source_response detail", rec.Body.String())
	}
}

func postDerive(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal request: %v", err)
	}
	mux := http.NewServeMux()
	(&Handler{}).Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/visualizations/derive", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeDeriveEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (querycontract.ResponseEnvelope, Packet) {
	t.Helper()

	var env querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v body=%s", err, rec.Body.String())
	}
	data, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("json.Marshal envelope data: %v", err)
	}
	var resp visualizationDeriveResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("json.Unmarshal derive response: %v data=%s", err, data)
	}
	return env, resp.Packet
}
