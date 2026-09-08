// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// serviceStoryAnswerData attaches the service.story answer packet companion
// to a service-story response body. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func serviceStoryAnswerData(serviceName string, data map[string]any, truth *TruthEnvelope) map[string]any {
	return querycontract.ServiceStoryAnswerData(serviceName, data, truth)
}

type incidentContextAnswerResponse struct {
	IncidentContextResponse
	AnswerPacket AnswerPacket `json:"answer_packet"`
}

func incidentContextAnswerData(incidentID string, response IncidentContextResponse, truth *TruthEnvelope) incidentContextAnswerResponse {
	envelope := &ResponseEnvelope{Data: response, Truth: truth, Error: nil}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "incident.context",
		Question:     fmt.Sprintf("Build incident context for %s.", incidentID),
		PrimaryTool:  "get_incident_context",
		PrimaryRoute: "/api/v0/incidents/{incident_id}/context",
		Summary:      response.Incident.Title,
		ResultRef:    "eshu://api-result/incidents/" + incidentID + "/context",
		Limitations:  incidentContextLimitations(response),
		Truncated:    response.Truncated,
		NoEvidence:   len(response.EvidencePath) == 0,
		Envelope:     envelope,
	})
	return incidentContextAnswerResponse{
		IncidentContextResponse: response,
		AnswerPacket:            packet,
	}
}

func incidentContextLimitations(response IncidentContextResponse) []string {
	if !response.Truncated {
		return nil
	}
	return []string{"incident context truncated; follow the bounded evidence path for more rows"}
}
