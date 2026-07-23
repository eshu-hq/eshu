// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/boundedset"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

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

// rawComponentEvidenceFromPayload decodes every field of the persisted
// "component_evidence" array (not only the 6 fields ComponentEvidenceRow
// exposes on the wire) into reducer.ComponentEvidence, so
// boundedComponentEvidenceRows can dedupe/sort/cap a legacy payload using the
// SAME complete-tuple identity the reducer's write path uses — decoding only
// the wire-exposed subset would make two distinct persisted rows (that
// differ only in a field ComponentEvidenceRow drops, e.g. lockfile_path)
// collapse into a false duplicate.
func rawComponentEvidenceFromPayload(raw any) []reducer.ComponentEvidence {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]reducer.ComponentEvidence, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, reducer.ComponentEvidence{
			ComponentID:      StringVal(row, "component_id"),
			Name:             StringVal(row, "name"),
			Version:          StringVal(row, "version"),
			PURL:             StringVal(row, "purl"),
			CPE:              StringVal(row, "cpe"),
			Ecosystem:        StringVal(row, "ecosystem"),
			LockfilePath:     StringVal(row, "lockfile_path"),
			DependencyScope:  StringVal(row, "dependency_scope"),
			DependencyType:   StringVal(row, "dependency_type"),
			ExtractionReason: StringVal(row, "extraction_reason"),
			FactID:           StringVal(row, "fact_id"),
		})
	}
	return out
}

// boundedComponentEvidenceRows applies a defensive READ-SIDE bound to the
// persisted "component_evidence" array: it re-runs the identical
// dedupe/sort/cap the reducer applies at write time
// (reducer.ComponentEvidenceLess/ComponentEvidenceTupleEqual via
// boundedset.DedupeSortCap, capped at reducer.MaxSBOMAttachmentComponentEvidenceRows)
// against whatever was ACTUALLY persisted, rather than trusting it was
// already bounded.
//
// This matters for a generation indexed before this bound existed: a legacy
// fact can carry an unbounded component_evidence array with a persisted
// component_count that (incorrectly, by the old unbounded write path) equals
// the full raw array length. Serving that payload verbatim would still
// return an oversized response with component_evidence_truncated == false —
// exactly what the write-time cap exists to prevent, just reached through an
// old payload instead of a new write. Applying the same bound here closes
// that gap with no migration/replay required: every read, old or new fact,
// goes through one cap.
//
// It returns the capped, deterministically ordered rows; the true total
// distinct-tuple count (max of the persisted component_count field and the
// raw persisted array length, so an inaccurate legacy count can't under-report);
// and whether the response is truncated relative to that true total. A fact
// already written post-cap (<=100 rows, accurate count) passes through
// unchanged: dedupe/sort of an already-deduped, already-sorted set is a
// no-op, and the true total equals len(rows), so truncated stays false.
func boundedComponentEvidenceRows(raw any, persistedCount int) ([]ComponentEvidenceRow, int, bool) {
	decoded := rawComponentEvidenceFromPayload(raw)
	rawArrayLen := len(decoded)

	capped, distinctCount := boundedset.DedupeSortCap(
		decoded,
		reducer.ComponentEvidenceLess,
		reducer.ComponentEvidenceTupleEqual,
		reducer.MaxSBOMAttachmentComponentEvidenceRows,
	)

	trueTotal := persistedCount
	if rawArrayLen > trueTotal {
		trueTotal = rawArrayLen
	}
	if distinctCount > trueTotal {
		trueTotal = distinctCount
	}

	out := make([]ComponentEvidenceRow, 0, len(capped))
	for _, component := range capped {
		out = append(out, ComponentEvidenceRow{
			ComponentID: component.ComponentID,
			Name:        component.Name,
			Version:     component.Version,
			PURL:        component.PURL,
			CPE:         component.CPE,
			FactID:      component.FactID,
		})
	}
	return out, trueTotal, trueTotal > len(out)
}
