// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file holds the bounded-evidence row decode helpers
// decodeSBOMAttestationAttachmentRow (sbom_attestation_attachments.go) calls:
// warning summary preview bounding, SLSA provenance materials (#5456), and
// component evidence. Split out to keep sbom_attestation_attachments.go under
// the package's 500-line cap.

func boundedSBOMWarningSummariesFromValue(raw any) ([]string, int, bool) {
	switch values := raw.(type) {
	case []string:
		return boundedSBOMWarningSummaries(values)
	case []any:
		return boundedSBOMWarningSummariesFromAny(values)
	default:
		return nil, 0, false
	}
}

func boundedSBOMWarningSummariesFromAny(values []any) ([]string, int, bool) {
	count := 0
	seen := map[string]struct{}{}
	preview := make([]string, 0, sbomAttestationWarningSummaryPreviewMaxCount)
	for _, value := range values {
		summary, ok := value.(string)
		if !ok {
			continue
		}
		count++
		if _, exists := seen[summary]; exists {
			continue
		}
		seen[summary] = struct{}{}
		if len(preview) < sbomAttestationWarningSummaryPreviewMaxCount {
			preview = append(preview, summary)
		}
	}
	return preview, count, count > len(preview)
}

func boundedSBOMWarningSummaries(values []string) ([]string, int, bool) {
	count := len(values)
	if count == 0 {
		return nil, 0, false
	}
	seen := map[string]struct{}{}
	preview := make([]string, 0, sbomAttestationWarningSummaryPreviewMaxCount)
	for _, summary := range values {
		if _, exists := seen[summary]; exists {
			continue
		}
		seen[summary] = struct{}{}
		if len(preview) < sbomAttestationWarningSummaryPreviewMaxCount {
			preview = append(preview, summary)
		}
	}
	return preview, count, count > len(preview)
}

// slsaMaterialRowsFromPayload decodes the reducer-persisted
// slsa_provenance_materials array (#5456) into the typed, bounded row set.
// The reducer already applies the write-time cap
// (maxSBOMAttachmentSLSAMaterialRows), so this only decodes what was
// persisted; truncation is reported by the caller comparing against the
// separately persisted full count. Each material's digest map is decoded via
// the existing stringMapVal(payload, key) helper (security_alert_reconciliation.go).
func slsaMaterialRowsFromPayload(raw any) []SLSAMaterialRow {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]SLSAMaterialRow, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SLSAMaterialRow{
			URI:    StringVal(row, "uri"),
			Digest: stringMapVal(row, "digest"),
		})
	}
	return out
}

func componentEvidenceRows(raw any) []ComponentEvidenceRow {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ComponentEvidenceRow, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, ComponentEvidenceRow{
			ComponentID: StringVal(row, "component_id"),
			Name:        StringVal(row, "name"),
			Version:     StringVal(row, "version"),
			PURL:        StringVal(row, "purl"),
			CPE:         StringVal(row, "cpe"),
			FactID:      StringVal(row, "fact_id"),
		})
	}
	return out
}
