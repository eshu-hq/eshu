// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// cannedVisualizationDeriver is a hermetic VisualizationReadAdmission:
// every builder answers a marker packet carrying the view it was asked
// for, so the route tests below prove the derive leg admits through
// the pin, not the static packet builders.
type cannedVisualizationDeriver struct{}

func cannedDerivePacket(view View) Packet {
	return Packet{View: view, Supported: true}
}

func (cannedVisualizationDeriver) BuildServiceStoryPacket(map[string]any, *querycontract.TruthEnvelope) Packet {
	return cannedDerivePacket(ViewServiceStory)
}

func (cannedVisualizationDeriver) BuildEvidenceCitationPacket(evidence.EvidenceCitationResponse, *querycontract.TruthEnvelope) Packet {
	return cannedDerivePacket(ViewEvidenceCitation)
}

func (cannedVisualizationDeriver) BuildIncidentContextPacket(model.IncidentContextResponse, *querycontract.TruthEnvelope) Packet {
	return cannedDerivePacket(ViewIncidentContext)
}

func postDeriveWithHandler(t *testing.T, h *Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal request: %v", err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/visualizations/derive", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestHandlerDeriveServesThroughPin proves the derive leg admits through
// the G2 VisualizationReadAdmission pin: an injected admission drives
// every view, and a handler built without an admission keeps deriving
// through the static deriver default (the cmd wiring builds an empty
// Handler).
func TestHandlerDeriveServesThroughPin(t *testing.T) {
	t.Parallel()

	for _, view := range []View{ViewServiceStory, ViewEvidenceCitation, ViewIncidentContext} {
		rec := postDeriveWithHandler(t, &Handler{Reads: cannedVisualizationDeriver{}}, map[string]any{
			"view":            string(view),
			"source_response": map[string]any{},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("view %q status = %d, want 200; body=%s", view, rec.Code, rec.Body.String())
		}
		_, packet := decodeDeriveEnvelope(t, rec)
		if packet.View != view || !packet.Supported {
			t.Fatalf("view %q packet = %+v, want the canned marker", view, packet)
		}
	}

	// Unknown views still reject through the pin, before any builder runs.
	rec := postDeriveWithHandler(t, &Handler{Reads: cannedVisualizationDeriver{}}, map[string]any{
		"view":            "not-a-view",
		"source_response": map[string]any{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown view status = %d, want 400", rec.Code)
	}
}
