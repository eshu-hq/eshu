package query

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// VisualizationHandler exposes pure visualization-packet derivation routes.
// It reads only the source response supplied by the caller and never queries
// graph, content, or reducer state.
type VisualizationHandler struct{}

type visualizationDeriveRequest struct {
	View           VisualizationView `json:"view"`
	SourceResponse json.RawMessage   `json:"source_response"`
	SourceTruth    *TruthEnvelope    `json:"source_truth,omitempty"`
}

type visualizationDeriveResponse struct {
	VisualizationPacket VisualizationPacket `json:"visualization_packet"`
}

// Mount registers visualization-packet derivation routes.
func (h *VisualizationHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/visualizations/derive", h.derive)
}

func (h *VisualizationHandler) derive(w http.ResponseWriter, r *http.Request) {
	var req visualizationDeriveRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	packet, err := deriveVisualizationPacket(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, visualizationDeriveResponse{
		VisualizationPacket: packet,
	}, visualizationPacketDerivationTruth(req.SourceTruth))
}

func deriveVisualizationPacket(req visualizationDeriveRequest) (VisualizationPacket, error) {
	view := VisualizationView(strings.TrimSpace(string(req.View)))
	switch view {
	case VisualizationViewServiceStory:
		var source map[string]any
		if err := decodeVisualizationSource(req.SourceResponse, &source); err != nil {
			return VisualizationPacket{}, err
		}
		if source == nil {
			source = map[string]any{}
		}
		return BuildServiceStoryVisualizationPacket(source, req.SourceTruth), nil
	case VisualizationViewEvidenceCitation:
		var source evidenceCitationResponse
		if err := decodeVisualizationSource(req.SourceResponse, &source); err != nil {
			return VisualizationPacket{}, err
		}
		return BuildEvidenceCitationVisualizationPacket(source, req.SourceTruth), nil
	case VisualizationViewIncidentContext:
		var source IncidentContextResponse
		if err := decodeVisualizationSource(req.SourceResponse, &source); err != nil {
			return VisualizationPacket{}, err
		}
		return BuildIncidentContextVisualizationPacket(source, req.SourceTruth), nil
	case "":
		return VisualizationPacket{}, fmt.Errorf("view is required")
	default:
		return VisualizationPacket{}, fmt.Errorf("view must be one of service_story, evidence_citation, or incident_context")
	}
}

func decodeVisualizationSource(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid source_response: %w", err)
	}
	return nil
}

func visualizationPacketDerivationTruth(source *TruthEnvelope) *TruthEnvelope {
	profile := ProfileLocalLightweight
	if source != nil && source.Profile != "" {
		profile = source.Profile
	}
	return BuildTruthEnvelope(
		profile,
		"visualization.packet_derivation",
		TruthBasisHybrid,
		"derived from caller-supplied authorized source response without graph or content reads",
	)
}
