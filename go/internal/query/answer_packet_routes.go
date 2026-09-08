// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// answerPacketCompanionInput aliases querycontract.AnswerPacketCompanionInput.
// The implementation moved to querycontract for #6060; this alias keeps root
// callers unchanged.
type answerPacketCompanionInput = querycontract.AnswerPacketCompanionInput

// withAnswerPacketCompanion forwards to
// querycontract.WithAnswerPacketCompanion. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func withAnswerPacketCompanion(
	data map[string]any,
	truth *TruthEnvelope,
	in answerPacketCompanionInput,
) map[string]any {
	return querycontract.WithAnswerPacketCompanion(data, truth, in)
}

func serviceStoryAnswerData(serviceName string, data map[string]any, truth *TruthEnvelope) map[string]any {
	return withAnswerPacketCompanion(data, truth, answerPacketCompanionInput{
		PromptFamily: "service.story",
		Question:     fmt.Sprintf("Tell the story for service %s.", serviceName),
		PrimaryTool:  "get_service_story",
		PrimaryRoute: "/api/v0/services/{service_name}/story",
		Summary:      StringVal(data, "story"),
		ResultRef:    "eshu://api-result/services/" + serviceName + "/story",
		Limitations:  stringSliceValue(data, "limitations"),
		Truncated:    BoolVal(mapValue(data, "result_limits"), "truncated"),
	})
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
