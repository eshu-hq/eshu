// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "fmt"

type answerPacketCompanionInput struct {
	PromptFamily         string
	Question             string
	PrimaryTool          string
	PrimaryRoute         string
	Summary              string
	ResultRef            string
	Limitations          []string
	Truncated            bool
	NoEvidence           bool
	EvidenceHandles      []evidenceCitationHandle
	RecommendedNextCalls []map[string]any
}

func withAnswerPacketCompanion(
	data map[string]any,
	truth *TruthEnvelope,
	in answerPacketCompanionInput,
) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data)+1)
	for key, value := range data {
		out[key] = value
	}
	envelope := &ResponseEnvelope{Data: data, Truth: truth, Error: nil}
	out["answer_packet"] = NewAnswerPacket(AnswerPacketInput{
		PromptFamily:         in.PromptFamily,
		Question:             in.Question,
		PrimaryTool:          in.PrimaryTool,
		PrimaryRoute:         in.PrimaryRoute,
		Summary:              in.Summary,
		ResultRef:            in.ResultRef,
		EmbedResult:          false,
		Limitations:          in.Limitations,
		Truncated:            in.Truncated,
		NoEvidence:           in.NoEvidence,
		EvidenceHandles:      in.EvidenceHandles,
		RecommendedNextCalls: in.RecommendedNextCalls,
		Envelope:             envelope,
	})
	return out
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

func codeTopicAnswerData(req codeTopicInvestigationRequest, data map[string]any, truth *TruthEnvelope) map[string]any {
	return withAnswerPacketCompanion(data, truth, answerPacketCompanionInput{
		PromptFamily:         "code.topic",
		Question:             req.Topic,
		PrimaryTool:          "investigate_code_topic",
		PrimaryRoute:         "/api/v0/code/topics/investigate",
		Summary:              codeTopicAnswerSummary(data),
		ResultRef:            "eshu://api-result/code/topics/investigate",
		Limitations:          codeTopicAnswerLimitations(data),
		Truncated:            BoolVal(data, "truncated"),
		NoEvidence:           IntVal(data, "count") == 0,
		EvidenceHandles:      codeTopicEvidenceHandles(data),
		RecommendedNextCalls: mapSliceValue(data, "recommended_next_calls"),
	})
}

func codeTopicAnswerSummary(data map[string]any) string {
	count := IntVal(data, "count")
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("Found %d ranked code-topic evidence group(s).", count)
}

func codeTopicAnswerLimitations(data map[string]any) []string {
	if !BoolVal(data, "truncated") {
		return nil
	}
	return []string{"result truncated; inspect additional pages before treating the evidence set as complete"}
}

func codeTopicEvidenceHandles(data map[string]any) []evidenceCitationHandle {
	groups := mapSliceValue(data, "evidence_groups")
	handles := make([]evidenceCitationHandle, 0, len(groups))
	for _, group := range groups {
		handle := mapValue(group, "source_handle")
		if len(handle) == 0 {
			continue
		}
		handles = append(handles, evidenceCitationHandle{
			Kind:         "source",
			RepoID:       StringVal(handle, "repo_id"),
			RelativePath: StringVal(handle, "relative_path"),
			StartLine:    IntVal(handle, "start_line"),
			EndLine:      IntVal(handle, "end_line"),
		})
	}
	return handles
}

func incidentContextLimitations(response IncidentContextResponse) []string {
	if !response.Truncated {
		return nil
	}
	return []string{"incident context truncated; follow the bounded evidence path for more rows"}
}
