// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Handler exposes pure visualization-packet derivation routes. It reads only
// the source response supplied by the caller and never queries graph,
// content, or reducer state.
type Handler struct {
	// Reads serves the packet derivation through the versioned #9669 G2
	// admission contract, never direct calls into the static packet
	// builders. A nil Reads keeps the standing static deriver, so the
	// cmd wiring (which builds an empty Handler) and every existing
	// caller keep deriving unchanged while the dest side cites the pin.
	Reads VisualizationReadAdmission
}

// reads resolves the pin the derive leg admits through: the injected
// admission when set, the static deriver otherwise.
func (h *Handler) reads() VisualizationReadAdmission {
	if h != nil && h.Reads != nil {
		return h.Reads
	}
	return visualizationStaticDeriver{}
}

type visualizationDeriveRequest struct {
	View           View                         `json:"view"`
	SourceResponse json.RawMessage              `json:"source_response"`
	SourceTruth    *querycontract.TruthEnvelope `json:"source_truth,omitempty"`
}

type visualizationDeriveResponse struct {
	Packet Packet `json:"visualization_packet"`
}

// Mount registers visualization-packet derivation routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/visualizations/derive", h.derive)
}

func (h *Handler) derive(w http.ResponseWriter, r *http.Request) {
	var req visualizationDeriveRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	packet, err := h.deriveVisualizationPacket(req)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, visualizationDeriveResponse{
		Packet: packet,
	}, visualizationPacketDerivationTruth(req.SourceTruth))
}

// deriveVisualizationPacket dispatches the derive through the G2
// VisualizationReadAdmission pin: each view admits through exactly the
// pinned static builder. The dispatch stays a method so the injected
// admission (or the static deriver default) backs every view.
func (h *Handler) deriveVisualizationPacket(req visualizationDeriveRequest) (Packet, error) {
	reads := h.reads()
	view := View(strings.TrimSpace(string(req.View)))
	switch view {
	case ViewServiceStory:
		var source map[string]any
		if err := decodeVisualizationSource(req.SourceResponse, &source); err != nil {
			return Packet{}, err
		}
		if source == nil {
			source = map[string]any{}
		}
		return reads.BuildServiceStoryPacket(source, req.SourceTruth), nil
	case ViewEvidenceCitation:
		var source evidence.EvidenceCitationResponse
		if err := decodeVisualizationSource(req.SourceResponse, &source); err != nil {
			return Packet{}, err
		}
		return reads.BuildEvidenceCitationPacket(source, req.SourceTruth), nil
	case ViewIncidentContext:
		var source model.IncidentContextResponse
		if err := decodeVisualizationSource(req.SourceResponse, &source); err != nil {
			return Packet{}, err
		}
		return reads.BuildIncidentContextPacket(source, req.SourceTruth), nil
	case "":
		return Packet{}, fmt.Errorf("view is required")
	default:
		return Packet{}, fmt.Errorf("view must be one of service_story, evidence_citation, or incident_context")
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

func visualizationPacketDerivationTruth(source *querycontract.TruthEnvelope) *querycontract.TruthEnvelope {
	profile := querycontract.ProfileLocalLightweight
	if source != nil && source.Profile != "" {
		profile = source.Profile
	}
	return querycontract.BuildTruthEnvelope(
		profile,
		PacketDerivationCapability,
		querycontract.TruthBasisHybrid,
		"derived from caller-supplied authorized source response without graph or content reads",
	)
}
