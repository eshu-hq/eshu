// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The incident-context answer-packet companion (#6060, lane B S2). These
// declarations moved here verbatim from the query root's
// answer_packet_routes.go with the incident handler: the handler above is
// their only caller, and the service-story companion that shared that file
// stays at the root.

// incidentContextAnswerResponse pairs one incident-context response with
// its answer packet companion.
type incidentContextAnswerResponse struct {
	model.IncidentContextResponse
	AnswerPacket querycontract.AnswerPacket `json:"answer_packet"`
}

func incidentContextAnswerData(incidentID string, response model.IncidentContextResponse, truth *querycontract.TruthEnvelope) incidentContextAnswerResponse {
	envelope := &querycontract.ResponseEnvelope{Data: response, Truth: truth, Error: nil}
	packet := querycontract.NewAnswerPacket(querycontract.AnswerPacketInput{
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

func incidentContextLimitations(response model.IncidentContextResponse) []string {
	if !response.Truncated {
		return nil
	}
	return []string{"incident context truncated; follow the bounded evidence path for more rows"}
}
