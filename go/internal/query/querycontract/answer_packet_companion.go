// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "fmt"

// AnswerPacketCompanionInput carries the composition inputs
// WithAnswerPacketCompanion needs to attach an answer_packet field onto an
// existing response body. The implementation moved from root's
// answer_packet_routes.go for #6060 so a handler-family subpackage can build
// the same companion input without importing root.
type AnswerPacketCompanionInput struct {
	PromptFamily         string
	Question             string
	PrimaryTool          string
	PrimaryRoute         string
	Summary              string
	ResultRef            string
	Limitations          []string
	Truncated            bool
	NoEvidence           bool
	EvidenceHandles      []EvidenceCitationHandle
	RecommendedNextCalls []map[string]any
}

// WithAnswerPacketCompanion returns a shallow copy of data with an
// "answer_packet" field built from in and truth. It never mutates data. The
// implementation moved from root's answer_packet_routes.go for #6060 so a
// handler-family subpackage can attach the same companion without importing
// root.
func WithAnswerPacketCompanion(
	data map[string]any,
	truth *TruthEnvelope,
	in AnswerPacketCompanionInput,
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

// CodeTopicAnswerSummary renders the one-line summary for a code-topic
// investigation's answer packet. The implementation moved from root's
// answer_packet_routes.go for #6060 so the code family can build the same
// summary without importing root.
func CodeTopicAnswerSummary(data map[string]any) string {
	count := IntVal(data, "count")
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("Found %d ranked code-topic evidence group(s).", count)
}

// CodeTopicAnswerLimitations renders the answer-packet limitations for a
// code-topic investigation whose result was truncated. The implementation
// moved from root's answer_packet_routes.go for #6060 so the code family can
// build the same limitations without importing root.
func CodeTopicAnswerLimitations(data map[string]any) []string {
	if !BoolVal(data, "truncated") {
		return nil
	}
	return []string{"result truncated; inspect additional pages before treating the evidence set as complete"}
}

// CodeTopicEvidenceHandles extracts the addressable evidence handles from a
// code-topic investigation's evidence_groups. The implementation moved from
// root's answer_packet_routes.go for #6060 so the code family can build the
// same handles without importing root.
func CodeTopicEvidenceHandles(data map[string]any) []EvidenceCitationHandle {
	groups := MapSliceValue(data, "evidence_groups")
	handles := make([]EvidenceCitationHandle, 0, len(groups))
	for _, group := range groups {
		handle := MapValue(group, "source_handle")
		if len(handle) == 0 {
			continue
		}
		handles = append(handles, EvidenceCitationHandle{
			Kind:         "source",
			RepoID:       StringVal(handle, "repo_id"),
			RelativePath: StringVal(handle, "relative_path"),
			StartLine:    IntVal(handle, "start_line"),
			EndLine:      IntVal(handle, "end_line"),
		})
	}
	return handles
}

// ServiceStoryAnswerData attaches the service.story answer packet companion
// to a service-story response body. The implementation moved from root's
// answer_packet_routes.go for #6060 so a handler-family subpackage can
// build the same companion without importing root.
func ServiceStoryAnswerData(serviceName string, data map[string]any, truth *TruthEnvelope) map[string]any {
	return WithAnswerPacketCompanion(data, truth, AnswerPacketCompanionInput{
		PromptFamily: "service.story",
		Question:     fmt.Sprintf("Tell the story for service %s.", serviceName),
		PrimaryTool:  "get_service_story",
		PrimaryRoute: "/api/v0/services/{service_name}/story",
		Summary:      StringVal(data, "story"),
		ResultRef:    "eshu://api-result/services/" + serviceName + "/story",
		Limitations:  StringSliceValue(data, "limitations"),
		Truncated:    BoolVal(MapValue(data, "result_limits"), "truncated"),
	})
}
