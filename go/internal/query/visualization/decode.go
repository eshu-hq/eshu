// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"encoding/json"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// BuildEvidenceCitationPacketFromMap derives an evidence-citation
// visualization from canonical response data decoded as map[string]any. It is
// the transport-facing adapter for HTTP, MCP, and CLI JSON surfaces; after
// decoding it delegates to BuildEvidenceCitationPacket, so the
// visualization remains a pure view over the source response.
func BuildEvidenceCitationPacketFromMap(
	response map[string]any,
	truth *querycontract.TruthEnvelope,
) Packet {
	var decoded querycontract.EvidenceCitationResponse
	if err := decodeVisualizationResponseMap(response, &decoded); err != nil {
		return unsupportedVisualizationPacket(
			ViewEvidenceCitation,
			truth,
			[]string{"evidence citation response could not be decoded for visualization"},
			evidenceCitationVisualizationNextCalls(querycontract.EvidenceCitationResponse{}),
		)
	}
	return BuildEvidenceCitationPacket(decoded, truth)
}

// BuildIncidentContextPacketFromMap derives an incident-context
// visualization from canonical response data decoded as map[string]any. It keeps
// transport surfaces from needing query-package internals while preserving the
// existing BuildIncidentContextPacket contract.
func BuildIncidentContextPacketFromMap(
	response map[string]any,
	truth *querycontract.TruthEnvelope,
) Packet {
	var decoded model.IncidentContextResponse
	if err := decodeVisualizationResponseMap(response, &decoded); err != nil {
		return unsupportedVisualizationPacket(
			ViewIncidentContext,
			truth,
			[]string{"incident context response could not be decoded for visualization"},
			incidentVisualizationNextCalls(),
		)
	}
	return BuildIncidentContextPacket(decoded, truth)
}

func decodeVisualizationResponseMap(response map[string]any, out any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
