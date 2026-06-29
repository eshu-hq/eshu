// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

func (h *CodeHandler) crossRepoDeadCodeRepositoryBoundaryEvidence(
	ctx context.Context,
	producerRepoID string,
) []crossRepoDeadCodeEvidence {
	readModel := loadRepositoryRelationshipReadModel(ctx, h.Content, producerRepoID)
	if readModel == nil {
		return nil
	}
	evidence := make([]crossRepoDeadCodeEvidence, 0, len(readModel.Relationships))
	for _, relationship := range readModel.Relationships {
		if StringVal(relationship, "direction") != "incoming" {
			continue
		}
		consumerRepoID := strings.TrimSpace(StringVal(relationship, "source_id"))
		if consumerRepoID == "" {
			continue
		}
		confidence := relationshipFloatVal(relationship, "confidence")
		evidence = append(evidence, crossRepoDeadCodeEvidence{
			ConsumerRepoID:   consumerRepoID,
			ConsumerRepoName: StringVal(relationship, "source_name"),
			RelationshipType: StringVal(relationship, "type"),
			EvidenceFamily:   "package_module_repo",
			Citation:         crossRepoDeadCodeRelationshipCitation(relationship),
			Confidence:       confidence,
			ConfidenceLabel:  crossRepoDeadCodeConfidenceLabel(confidence),
			ResolutionMethod: firstNonEmptyString(
				StringVal(relationship, "resolution_source"),
				StringVal(relationship, "confidence_basis"),
			),
			GenerationID:     StringVal(relationship, "generation_id"),
			GenerationStatus: "active",
			NeedsEvidence:    true,
			Reason:           "package_module_repo_needs_symbol_evidence",
		})
	}
	return evidence
}

func crossRepoDeadCodeRelationshipCitation(relationship map[string]any) string {
	generationID := strings.TrimSpace(StringVal(relationship, "generation_id"))
	resolvedID := strings.TrimSpace(StringVal(relationship, "resolved_id"))
	if resolvedID != "" {
		return "repository_relationships:" + generationID + "/" + resolvedID
	}
	return "repository_relationships:" +
		generationID + "/" +
		StringVal(relationship, "source_id") + "->" +
		StringVal(relationship, "target_id")
}

func crossRepoDeadCodeAnySlice(rows []map[string]any) []any {
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		values = append(values, row)
	}
	return values
}

func crossRepoDeadCodeBucketCounts(buckets map[string]any) map[string]any {
	counts := make(map[string]any, len(buckets))
	for key, raw := range buckets {
		if rows, ok := raw.([]any); ok {
			counts[key] = len(rows)
		}
	}
	return counts
}

func crossRepoDeadCodeAnalysisRows(buckets map[string]any) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, key := range []string{"dead", "live_by_consumer", "unknown"} {
		rawRows, _ := buckets[key].([]any)
		for _, raw := range rawRows {
			row, ok := raw.(map[string]any)
			if ok {
				rows = append(rows, row)
			}
		}
	}
	return rows
}
