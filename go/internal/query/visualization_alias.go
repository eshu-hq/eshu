// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: type aliases and thin forwarders for the moved visualization family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import "github.com/eshu-hq/eshu/go/internal/query/visualization"

// visualization_alias.go is the root alias shim for the visualization-packet
// derivation handler family (#6642 Part A, modelled on language_alias.go).
// Handler, its Mount method, and the derivation builders moved to
// visualization/. Names the rest of the program still spells `query.X`
// (handler wiring, cmd routers, the staying root
// visualization_packet_surface_test.go, and internal/mcp's
// answer_parity_gen2_test.go) alias here so the move touches no caller
// outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import visualization
// directly.

// VisualizationHandler is the visualization-packet derivation handler
// family type. Its home is visualization/; this alias keeps cmd/api's and
// cmd/mcp-server's wiring_router.go struct literals, handler.go's APIRouter
// field, and internal/mcp's visualization_packet_surface_test.go spelling
// query.VisualizationHandler unchanged.
type VisualizationHandler = visualization.Handler

// VisualizationPacket is a compact, bounded, derived view of an existing
// story, evidence-citation, or incident-context query response. Its home is
// visualization/; this alias keeps the staying
// visualization_packet_surface_test.go and internal/mcp's
// answer_parity_gen2_test.go spelling query.VisualizationPacket unchanged.
type VisualizationPacket = visualization.Packet

// VisualizationView names the derived-view family a packet was built from.
// Its home is visualization/; this alias keeps the staying
// visualization_packet_surface_test.go spelling the package-local name
// unchanged.
type VisualizationView = visualization.View

const (
	// VisualizationViewServiceStory is the service-story dossier subgraph.
	VisualizationViewServiceStory = visualization.ViewServiceStory
	// VisualizationViewEvidenceCitation is the evidence-citation subgraph.
	VisualizationViewEvidenceCitation = visualization.ViewEvidenceCitation
	// VisualizationViewIncidentContext is the incident-context subgraph.
	VisualizationViewIncidentContext = visualization.ViewIncidentContext
	// VisualizationViewUnsupported marks a packet with no derivable subgraph.
	VisualizationViewUnsupported = visualization.ViewUnsupported
)

// BuildServiceStoryVisualizationPacket forwards to
// visualization.BuildServiceStoryPacket. Its home is
// visualization/; this wrapper keeps internal/mcp's
// answer_parity_gen2_test.go calling the package-local name.
func BuildServiceStoryVisualizationPacket(response map[string]any, truth *TruthEnvelope) VisualizationPacket {
	return visualization.BuildServiceStoryPacket(response, truth)
}

// BuildEvidenceCitationVisualizationPacketFromMap forwards to
// visualization.BuildEvidenceCitationPacketFromMap. Its home is
// visualization/; this wrapper keeps internal/mcp's
// answer_parity_gen2_test.go calling the package-local name.
func BuildEvidenceCitationVisualizationPacketFromMap(response map[string]any, truth *TruthEnvelope) VisualizationPacket {
	return visualization.BuildEvidenceCitationPacketFromMap(response, truth)
}

// BuildIncidentContextVisualizationPacketFromMap forwards to
// visualization.BuildIncidentContextPacketFromMap. Its home is
// visualization/; this wrapper keeps internal/mcp's
// answer_parity_gen2_test.go calling the package-local name.
func BuildIncidentContextVisualizationPacketFromMap(response map[string]any, truth *TruthEnvelope) VisualizationPacket {
	return visualization.BuildIncidentContextPacketFromMap(response, truth)
}
