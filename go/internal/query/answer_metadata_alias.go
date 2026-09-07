// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// AnswerMetadata is the normalized, additive answer companion attached to
// story and investigation responses. The implementation moved to
// internal/query/querycontract (#6060) so handler-family subpackages can
// attach it without importing this package; this alias keeps root's own
// handlers, packet builders, and tests compiling unchanged.
type AnswerMetadata = querycontract.AnswerMetadata

// attachAnswerMetadata derives the normalized answer companion from an
// already-built response payload and stores it under "answer_metadata". The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func attachAnswerMetadata(data map[string]any) map[string]any {
	return querycontract.AttachAnswerMetadata(data)
}

// BuildAnswerMetadata derives normalized answer metadata from an
// already-built response payload. The implementation moved to querycontract
// for #6060; this wrapper keeps root callers unchanged.
func BuildAnswerMetadata(data map[string]any) AnswerMetadata {
	return querycontract.BuildAnswerMetadata(data)
}

// AnswerMetadataFromData extracts normalized answer metadata from a response
// map. The implementation moved to querycontract for #6060; this wrapper
// keeps root callers unchanged.
func AnswerMetadataFromData(data map[string]any) (AnswerMetadata, bool) {
	return querycontract.AnswerMetadataFromData(data)
}

func incidentContextAnswerMetadata(response IncidentContextResponse) AnswerMetadata {
	missing := make([]map[string]any, 0, len(response.MissingEvidence))
	for _, item := range response.MissingEvidence {
		missing = append(missing, map[string]any{
			"slot":   string(item.Slot),
			"reason": item.Reason,
		})
	}
	evidence := make([]map[string]any, 0)
	for _, edge := range response.EvidencePath {
		for _, ref := range edge.Evidence {
			if ref.FactID == "" {
				continue
			}
			evidence = append(evidence, map[string]any{
				"kind":            "fact",
				"evidence_family": string(edge.Slot),
				"entity_id":       ref.FactID,
				"reason":          edge.Explanation,
			})
		}
	}
	data := map[string]any{
		"evidence_handles": evidence,
		"missing_evidence": missing,
		"truncated":        response.Truncated,
		"coverage": map[string]any{
			"query_shape":          "incident_context_evidence_path",
			"evidence_path_slots":  len(response.EvidencePath),
			"missing_evidence":     len(response.MissingEvidence),
			"ambiguous_evidence":   len(response.AmbiguousEvidence),
			"related_change_count": len(response.RelatedChanges),
			"truncated":            response.Truncated,
		},
	}
	return querycontract.BuildAnswerMetadata(data)
}
