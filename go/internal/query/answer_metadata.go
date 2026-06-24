// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

const answerMetadataSchemaVersion = "answer_metadata.v1"

// AnswerMetadata is the normalized, additive answer companion attached to
// story and investigation responses. It summarizes existing route metadata so
// prompt surfaces can reason about evidence, gaps, truncation, coverage, and
// follow-up calls without parsing route-specific fields.
type AnswerMetadata struct {
	SchemaVersion        string           `json:"schema_version"`
	EvidenceHandles      []map[string]any `json:"evidence_handles"`
	MissingEvidence      []map[string]any `json:"missing_evidence"`
	Limitations          []map[string]any `json:"limitations"`
	Truncated            bool             `json:"truncated"`
	Coverage             map[string]any   `json:"coverage"`
	PartialReasons       []string         `json:"partial_reasons"`
	RecommendedNextCalls []map[string]any `json:"recommended_next_calls"`
}

func attachAnswerMetadata(data map[string]any) map[string]any {
	if data == nil {
		return data
	}
	data["answer_metadata"] = BuildAnswerMetadata(data)
	return data
}

// BuildAnswerMetadata derives normalized answer metadata from an already-built
// response payload. It does not fetch, infer, or mutate source truth.
func BuildAnswerMetadata(data map[string]any) AnswerMetadata {
	coverage := answerMetadataCoverage(data)
	truncated := BoolVal(data, "truncated") || BoolVal(coverage, "truncated")
	metadata := AnswerMetadata{
		SchemaVersion:        answerMetadataSchemaVersion,
		EvidenceHandles:      answerMetadataEvidenceHandles(data),
		MissingEvidence:      answerMetadataMissingEvidence(data),
		Limitations:          answerMetadataLimitations(data),
		Truncated:            truncated,
		Coverage:             coverage,
		RecommendedNextCalls: answerMetadataNextCalls(data),
	}
	metadata.PartialReasons = answerMetadataPartialReasons(metadata)
	return metadata
}

// AnswerMetadataFromData extracts normalized answer metadata from a response
// map, tolerating both in-process structs and JSON-decoded maps.
func AnswerMetadataFromData(data map[string]any) (AnswerMetadata, bool) {
	if data == nil {
		return AnswerMetadata{}, false
	}
	return answerMetadataFromRaw(data["answer_metadata"])
}

func answerMetadataFromRaw(raw any) (AnswerMetadata, bool) {
	switch typed := raw.(type) {
	case AnswerMetadata:
		return typed.withDefaults(), true
	case map[string]any:
		metadata := AnswerMetadata{
			SchemaVersion:        StringVal(typed, "schema_version"),
			EvidenceHandles:      metadataMapRows(typed["evidence_handles"]),
			MissingEvidence:      metadataMapRows(typed["missing_evidence"]),
			Limitations:          metadataMapRows(typed["limitations"]),
			Truncated:            BoolVal(typed, "truncated"),
			Coverage:             copyMap(mapValue(typed, "coverage")),
			PartialReasons:       StringSliceVal(typed, "partial_reasons"),
			RecommendedNextCalls: metadataMapRows(typed["recommended_next_calls"]),
		}
		return metadata.withDefaults(), metadata.SchemaVersion != ""
	default:
		return AnswerMetadata{}, false
	}
}

func (metadata AnswerMetadata) withDefaults() AnswerMetadata {
	if metadata.SchemaVersion == "" {
		metadata.SchemaVersion = answerMetadataSchemaVersion
	}
	if metadata.EvidenceHandles == nil {
		metadata.EvidenceHandles = []map[string]any{}
	}
	if metadata.MissingEvidence == nil {
		metadata.MissingEvidence = []map[string]any{}
	}
	if metadata.Limitations == nil {
		metadata.Limitations = []map[string]any{}
	}
	if metadata.Coverage == nil {
		metadata.Coverage = map[string]any{}
	}
	if metadata.PartialReasons == nil {
		metadata.PartialReasons = []string{}
	}
	if metadata.RecommendedNextCalls == nil {
		metadata.RecommendedNextCalls = []map[string]any{}
	}
	return metadata
}

func answerMetadataCoverage(data map[string]any) map[string]any {
	for _, key := range []string{"coverage", "coverage_summary", "result_limits"} {
		if coverage := mapValue(data, key); len(coverage) > 0 {
			return copyMap(coverage)
		}
	}
	return map[string]any{}
}

func answerMetadataEvidenceHandles(data map[string]any) []map[string]any {
	handles := make([]map[string]any, 0)
	handles = appendMetadataHandles(handles, data["evidence_handles"])
	handles = appendMetadataHandles(handles, data["call_graph_handles"])
	handles = appendSourceHandles(handles, data["evidence_groups"])
	handles = appendSourceHandles(handles, data["matched_files"])
	handles = appendSourceHandles(handles, data["matched_symbols"])
	handles = appendNestedSourceHandles(handles, mapValue(data, "code_surface"), "changed_files", "touched_symbols", "evidence_groups")
	handles = appendEvidenceHandleFields(handles, data["direct_impact"])
	handles = appendEvidenceHandleFields(handles, data["transitive_impact"])
	return dedupeMetadataRows(handles, "kind", "repo_id", "relative_path", "entity_id", "start_line", "end_line", "reason")
}

func appendNestedSourceHandles(
	handles []map[string]any,
	parent map[string]any,
	keys ...string,
) []map[string]any {
	for _, key := range keys {
		handles = appendSourceHandles(handles, parent[key])
	}
	return handles
}

func appendSourceHandles(handles []map[string]any, raw any) []map[string]any {
	for _, row := range metadataMapRows(raw) {
		handles = appendMetadataHandles(handles, row["source_handle"])
	}
	return handles
}

func appendEvidenceHandleFields(handles []map[string]any, raw any) []map[string]any {
	for _, row := range metadataMapRows(raw) {
		handles = appendMetadataHandles(handles, row["evidence_handle"])
	}
	return handles
}

func appendMetadataHandles(handles []map[string]any, raw any) []map[string]any {
	for _, row := range metadataMapRows(raw) {
		handle := normalizeMetadataHandle(row)
		if len(handle) > 0 {
			handles = append(handles, handle)
		}
	}
	return handles
}

func normalizeMetadataHandle(row map[string]any) map[string]any {
	handle := map[string]any{}
	for _, key := range []string{"kind", "repo_id", "relative_path", "entity_id", "evidence_family", "reason"} {
		if value := strings.TrimSpace(StringVal(row, key)); value != "" {
			handle[key] = value
		}
	}
	for _, key := range []string{"start_line", "end_line"} {
		if value := IntVal(row, key); value > 0 {
			handle[key] = value
		}
	}
	if _, ok := handle["kind"]; !ok {
		switch {
		case handle["entity_id"] != nil:
			handle["kind"] = "entity"
		case handle["repo_id"] != nil && handle["relative_path"] != nil:
			handle["kind"] = "file"
		}
	}
	if handle["entity_id"] == nil && (handle["repo_id"] == nil || handle["relative_path"] == nil) {
		return nil
	}
	return handle
}

func answerMetadataMissingEvidence(data map[string]any) []map[string]any {
	missing := metadataReasonRows(data["missing_evidence"])
	missing = append(missing, nestedReasonRows(data, "support_overview", "target_support", "missing_evidence")...)
	missing = append(missing, nestedReasonRows(data, "documentation_overview", "target_documentation", "missing_evidence")...)
	return dedupeMetadataRows(missing, "kind", "slot", "reason", "repo_id", "relative_path", "entity_id")
}

func nestedReasonRows(data map[string]any, keys ...string) []map[string]any {
	current := data
	for _, key := range keys[:len(keys)-1] {
		current = mapValue(current, key)
		if current == nil {
			return nil
		}
	}
	return metadataReasonRows(current[keys[len(keys)-1]])
}

func answerMetadataLimitations(data map[string]any) []map[string]any {
	limitations := metadataReasonRows(data["limitations"])
	limitations = append(limitations, metadataReasonRows(mapValue(data, "service_identity")["limitations"])...)
	if BoolVal(data, "truncated") {
		limitations = append(limitations, map[string]any{
			"kind":   "result_truncated",
			"reason": "result truncated; not all evidence is included",
		})
	}
	return dedupeMetadataRows(limitations, "kind", "reason")
}

func answerMetadataNextCalls(data map[string]any) []map[string]any {
	return dedupeMetadataRows(metadataMapRows(data["recommended_next_calls"]), "tool", "route", "reason")
}

func answerMetadataPartialReasons(metadata AnswerMetadata) []string {
	reasons := make([]string, 0)
	if metadata.Truncated {
		reasons = appendUniqueReason(reasons, "result_truncated")
	}
	if len(metadata.MissingEvidence) > 0 {
		reasons = appendUniqueReason(reasons, "missing_evidence")
	}
	if state := strings.TrimSpace(StringVal(metadata.Coverage, "state")); state != "" {
		switch state {
		case "complete", "exact", "fresh", "supported":
		default:
			reasons = appendUniqueReason(reasons, "coverage_"+state)
		}
	}
	for _, limitation := range metadata.Limitations {
		reason := StringVal(limitation, "kind")
		if reason == "" {
			reason = StringVal(limitation, "reason")
		}
		reasons = appendUniqueReason(reasons, reason)
	}
	return reasons
}

func metadataReasonRows(raw any) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, row := range metadataMapRows(raw) {
		if len(row) == 0 {
			continue
		}
		out := copyMap(row)
		if out["reason"] == nil {
			for _, key := range []string{"explanation", "message", "slot", "kind"} {
				if reason := strings.TrimSpace(StringVal(out, key)); reason != "" {
					out["reason"] = reason
					break
				}
			}
		}
		rows = append(rows, out)
	}
	return rows
}

func metadataMapRows(raw any) []map[string]any {
	switch typed := raw.(type) {
	case nil:
		return []map[string]any{}
	case map[string]any:
		return []map[string]any{copyMap(typed)}
	case []map[string]any:
		rows := make([]map[string]any, 0, len(typed))
		for _, row := range typed {
			rows = append(rows, copyMap(row))
		}
		return rows
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			switch value := item.(type) {
			case map[string]any:
				rows = append(rows, copyMap(value))
			case string:
				if text := strings.TrimSpace(value); text != "" {
					rows = append(rows, map[string]any{"reason": text})
				}
			}
		}
		return rows
	case []string:
		rows := make([]map[string]any, 0, len(typed))
		for _, value := range typed {
			if text := strings.TrimSpace(value); text != "" {
				rows = append(rows, map[string]any{"reason": text})
			}
		}
		return rows
	default:
		if text := strings.TrimSpace(fmt.Sprintf("%v", typed)); text != "" {
			return []map[string]any{{"reason": text}}
		}
		return []map[string]any{}
	}
}

func dedupeMetadataRows(rows []map[string]any, keys ...string) []map[string]any {
	if rows == nil {
		return []map[string]any{}
	}
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		key := metadataRowKey(row, keys...)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return metadataRowKey(out[i], keys...) < metadataRowKey(out[j], keys...)
	})
	return out
}

func metadataRowKey(row map[string]any, keys ...string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strings.TrimSpace(fmt.Sprintf("%v", row[key])))
	}
	joined := strings.Join(parts, "|")
	if strings.Trim(joined, "| ") == "" {
		return ""
	}
	return joined
}

func appendUniqueReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
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
	return BuildAnswerMetadata(data)
}
