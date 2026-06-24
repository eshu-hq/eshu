// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "encoding/json"

// BuildEvidenceCitationVisualizationPacketFromMap derives an evidence-citation
// visualization from canonical response data decoded as map[string]any. It is
// the transport-facing adapter for HTTP, MCP, and CLI JSON surfaces; after
// decoding it delegates to BuildEvidenceCitationVisualizationPacket, so the
// visualization remains a pure view over the source response.
func BuildEvidenceCitationVisualizationPacketFromMap(
	response map[string]any,
	truth *TruthEnvelope,
) VisualizationPacket {
	var decoded evidenceCitationResponse
	if err := decodeVisualizationResponseMap(response, &decoded); err != nil {
		return unsupportedVisualizationPacket(
			VisualizationViewEvidenceCitation,
			truth,
			[]string{"evidence citation response could not be decoded for visualization"},
			evidenceCitationVisualizationNextCalls(evidenceCitationResponse{}),
		)
	}
	return BuildEvidenceCitationVisualizationPacket(decoded, truth)
}

// BuildIncidentContextVisualizationPacketFromMap derives an incident-context
// visualization from canonical response data decoded as map[string]any. It keeps
// transport surfaces from needing query-package internals while preserving the
// existing BuildIncidentContextVisualizationPacket contract.
func BuildIncidentContextVisualizationPacketFromMap(
	response map[string]any,
	truth *TruthEnvelope,
) VisualizationPacket {
	var decoded IncidentContextResponse
	if err := decodeVisualizationResponseMap(response, &decoded); err != nil {
		return unsupportedVisualizationPacket(
			VisualizationViewIncidentContext,
			truth,
			[]string{"incident context response could not be decoded for visualization"},
			incidentVisualizationNextCalls(),
		)
	}
	return BuildIncidentContextVisualizationPacket(decoded, truth)
}

func decodeVisualizationResponseMap(response map[string]any, out any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
