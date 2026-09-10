// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func (a *Analyzer) crossRepoDeadCodeRepositoryBoundaryEvidence(
	ctx context.Context,
	producerRepoID string,
) []CrossRepoDeadCodeEvidence {
	readModel := querycontract.LoadRepositoryRelationshipReadModel(ctx, a.deps.Content, producerRepoID)
	if readModel == nil {
		return nil
	}
	evidence := make([]CrossRepoDeadCodeEvidence, 0, len(readModel.Relationships))
	for _, relationship := range readModel.Relationships {
		if querycontract.StringVal(relationship, "direction") != "incoming" {
			continue
		}
		consumerRepoID := strings.TrimSpace(querycontract.StringVal(relationship, "source_id"))
		if consumerRepoID == "" {
			continue
		}
		confidence := querycontract.FloatVal(relationship, "confidence")
		evidence = append(evidence, CrossRepoDeadCodeEvidence{
			ConsumerRepoID:   consumerRepoID,
			ConsumerRepoName: querycontract.StringVal(relationship, "source_name"),
			RelationshipType: querycontract.StringVal(relationship, "type"),
			EvidenceFamily:   "package_module_repo",
			Citation:         crossRepoDeadCodeRelationshipCitation(relationship),
			Confidence:       confidence,
			ConfidenceLabel:  CrossRepoDeadCodeConfidenceLabel(confidence),
			ResolutionMethod: querycontract.FirstNonEmptyString(
				querycontract.StringVal(relationship, "resolution_source"),
				querycontract.StringVal(relationship, "confidence_basis"),
			),
			GenerationID:     querycontract.StringVal(relationship, "generation_id"),
			GenerationStatus: "active",
			NeedsEvidence:    true,
			Reason:           "package_module_repo_needs_symbol_evidence",
		})
	}
	return evidence
}

func crossRepoDeadCodeRelationshipCitation(relationship map[string]any) string {
	generationID := strings.TrimSpace(querycontract.StringVal(relationship, "generation_id"))
	resolvedID := strings.TrimSpace(querycontract.StringVal(relationship, "resolved_id"))
	if resolvedID != "" {
		return "repository_relationships:" + generationID + "/" + resolvedID
	}
	return "repository_relationships:" +
		generationID + "/" +
		querycontract.StringVal(relationship, "source_id") + "->" +
		querycontract.StringVal(relationship, "target_id")
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

func cloneCrossRepoDeadCodeResult(row map[string]any) map[string]any {
	clone := make(map[string]any, len(row)+4)
	for key, value := range row {
		clone[key] = value
	}
	return clone
}
