// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// attachPreChangeImpactPacket attaches the pre-change answer packet to an
// already-built impact payload. It is the production implementation behind
// impact.AttachPreChangeImpactPacket (wired in family_impact_shim.go); the
// split mirrors attachDeveloperChangePlanPacket because packet composition
// names the root AnswerPacket cluster, which cannot cross into the impact
// subpackage. See #6060.
func attachPreChangeImpactPacket(data map[string]any, summary string, truth *querycontract.TruthEnvelope) map[string]any {
	metadata, _ := AnswerMetadataFromData(data)
	envelope := &querycontract.ResponseEnvelope{Data: data, Truth: truth, Error: nil}
	data["answer_packet"] = NewAnswerPacketFromMetadata(AnswerPacketInput{
		PromptFamily: "pre_change.impact",
		Question:     "What does this change affect, what evidence proves it, and what should I inspect next?",
		PrimaryTool:  "analyze_pre_change_impact",
		PrimaryRoute: "/api/v0/impact/pre-change",
		Summary:      summary,
		ResultRef:    "eshu://api-result/impact/pre-change",
		Envelope:     envelope,
	}, metadata)
	return data
}

// attachDeveloperChangePlanPacket attaches the developer-change-plan answer
// packet to an already-built plan payload. It lives in root (not in the
// impact subpackage) because packet composition names the root AnswerPacket
// cluster, which cannot cross into that package. The plan summary stays
// caller-built alongside the payload. See #6060.
func attachDeveloperChangePlanPacket(data map[string]any, summary string, truth *querycontract.TruthEnvelope) map[string]any {
	metadata, _ := AnswerMetadataFromData(data)
	envelope := &querycontract.ResponseEnvelope{Data: data, Truth: truth, Error: nil}
	data["answer_packet"] = NewAnswerPacketFromMetadata(AnswerPacketInput{
		PromptFamily: "developer.change_plan",
		Question:     "What safe, read-only action plan should guide this change?",
		PrimaryTool:  "plan_developer_change",
		PrimaryRoute: "/api/v0/impact/developer-change-plan",
		Summary:      summary,
		ResultRef:    "eshu://api-result/impact/developer-change-plan",
		Envelope:     envelope,
	}, metadata)
	return data
}

// NewAnswerPacketFromMetadata composes an AnswerPacket from normalized
// AnswerMetadata. The metadata fills the packet's evidence, missing-evidence,
// limitation, truncation, and follow-up slots without route-specific parsing.
func NewAnswerPacketFromMetadata(in AnswerPacketInput, metadata AnswerMetadata) AnswerPacket {
	metadata = metadata.WithDefaults()
	if len(in.EvidenceHandles) == 0 {
		in.EvidenceHandles = citationHandlesFromMetadata(metadata.EvidenceHandles)
	}
	if len(in.MissingEvidence) == 0 {
		in.MissingEvidence = missingCitationHandlesFromMetadata(metadata.MissingEvidence)
	}
	if len(in.Limitations) == 0 {
		in.Limitations = limitationStringsFromMetadata(metadata.Limitations)
	}
	if len(in.RecommendedNextCalls) == 0 {
		in.RecommendedNextCalls = metadata.RecommendedNextCalls
	}
	in.Truncated = in.Truncated || metadata.Truncated
	if len(in.EvidenceHandles) == 0 && (len(metadata.MissingEvidence) > 0 || BoolVal(metadata.Coverage, "empty")) {
		in.NoEvidence = true
	}
	return NewAnswerPacket(in)
}

func citationHandlesFromMetadata(rows []map[string]any) []evidenceCitationHandle {
	if len(rows) == 0 {
		return nil
	}
	handles := make([]evidenceCitationHandle, 0, len(rows))
	for _, row := range rows {
		handle := evidenceCitationHandle{
			Kind:           StringVal(row, "kind"),
			RepoID:         StringVal(row, "repo_id"),
			RelativePath:   StringVal(row, "relative_path"),
			EntityID:       StringVal(row, "entity_id"),
			EvidenceFamily: StringVal(row, "evidence_family"),
			Reason:         StringVal(row, "reason"),
			StartLine:      IntVal(row, "start_line"),
			EndLine:        IntVal(row, "end_line"),
		}
		if handle.Kind == "" {
			if handle.EntityID != "" {
				handle.Kind = "entity"
			} else if handle.RepoID != "" && handle.RelativePath != "" {
				handle.Kind = "file"
			}
		}
		if handle.EntityID == "" && (handle.RepoID == "" || handle.RelativePath == "") {
			continue
		}
		handles = append(handles, handle)
	}
	return handles
}

func missingCitationHandlesFromMetadata(rows []map[string]any) []evidenceCitationHandle {
	if len(rows) == 0 {
		return nil
	}
	handles := make([]evidenceCitationHandle, 0, len(rows))
	for _, row := range rows {
		handle := evidenceCitationHandle{
			Kind:           StringVal(row, "kind"),
			RepoID:         StringVal(row, "repo_id"),
			RelativePath:   StringVal(row, "relative_path"),
			EntityID:       StringVal(row, "entity_id"),
			EvidenceFamily: querycontract.FirstNonEmptyString(StringVal(row, "evidence_family"), StringVal(row, "slot")),
			Reason:         StringVal(row, "reason"),
			StartLine:      IntVal(row, "start_line"),
			EndLine:        IntVal(row, "end_line"),
		}
		if handle.Kind == "" {
			switch {
			case handle.EntityID != "":
				handle.Kind = "entity"
			case handle.RepoID != "" && handle.RelativePath != "":
				handle.Kind = "file"
			case handle.EvidenceFamily != "":
				handle.Kind = "evidence"
			}
		}
		if handle.Kind == "" && handle.Reason == "" {
			continue
		}
		handles = append(handles, handle)
	}
	return handles
}

func limitationStringsFromMetadata(rows []map[string]any) []string {
	if len(rows) == 0 {
		return nil
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		reason := StringVal(row, "reason")
		if reason == "" {
			reason = StringVal(row, "kind")
		}
		values = appendReason(values, reason)
	}
	return values
}
