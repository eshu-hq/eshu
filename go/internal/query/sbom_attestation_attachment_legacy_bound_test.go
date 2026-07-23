// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestDecodeSBOMAttestationAttachmentRowBoundsLegacyUnboundedComponentEvidence
// proves the read-side defensive bound (boundedComponentEvidenceRows) closes
// the gap the reducer's write-time-only cap left open: a generation indexed
// BEFORE that cap existed can have a persisted component_evidence array
// larger than the cap, with a persisted component_count field that
// (correctly, by the old unbounded write path) equals that same oversized
// raw length. Decoding that legacy shape through the production query path
// must still return a capped, deterministically ordered page and an honest
// component_count/component_evidence_truncated pair — with no migration or
// replay, because decodeSBOMAttestationAttachmentRow re-bounds whatever was
// actually persisted on every read.
func TestDecodeSBOMAttestationAttachmentRowBoundsLegacyUnboundedComponentEvidence(t *testing.T) {
	t.Parallel()

	const legacyRawCount = 150 // pre-#5412 shape: raw array > the write-time cap (100)
	components := make([]any, 0, legacyRawCount)
	for i := 0; i < legacyRawCount; i++ {
		components = append(components, map[string]any{
			"component_id": fmt.Sprintf("component-%03d", i),
			"name":         fmt.Sprintf("component-%03d", i),
			"fact_id":      fmt.Sprintf("fact-%03d", i),
		})
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"document_id":       "doc-legacy-unbounded-components",
		"attachment_status": "attached_parse_only",
		// The legacy (pre-#5412) write path never deduped, sorted, or
		// capped: component_count was simply len(components), so it equals
		// the raw array length exactly, even though that length exceeds
		// the cap this change introduced.
		"component_count":    legacyRawCount,
		"component_evidence": components,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSBOMAttestationAttachmentRow("attachment-legacy-unbounded-components", "reported", payloadBytes)
	if err != nil {
		t.Fatalf("decodeSBOMAttestationAttachmentRow() error = %v", err)
	}
	if got, want := len(row.ComponentEvidence), reducer.MaxSBOMAttachmentComponentEvidenceRows; got != want {
		t.Fatalf("ComponentEvidence len = %d, want read-side cap %d (legacy oversized payload was served unbounded)", got, want)
	}
	if got, want := row.ComponentCount, legacyRawCount; got != want {
		t.Fatalf("ComponentCount = %d, want true total %d (the honest count must not silently shrink to the capped row count)", got, want)
	}
	if !row.ComponentEvidenceTruncated {
		t.Fatal("ComponentEvidenceTruncated = false, want true: a legacy payload whose raw array exceeds the cap must report truncation even though its own persisted component_count equals the (oversized) raw length")
	}
	// Deterministic, sorted order: component-000..component-099 survive the
	// cap (lexicographic on component_id here, since every other tuple field
	// is blank and equal).
	if got, want := row.ComponentEvidence[0].ComponentID, "component-000"; got != want {
		t.Fatalf("first row component_id = %q, want %q", got, want)
	}
	if got, want := row.ComponentEvidence[len(row.ComponentEvidence)-1].ComponentID, "component-099"; got != want {
		t.Fatalf("last row component_id = %q, want %q", got, want)
	}
}

// TestDecodeSBOMAttestationAttachmentRowServesPostCapFactUnchanged proves the
// read-side defensive bound is idempotent: a fact written AFTER the
// write-time cap existed (<=100 rows, an accurate persisted component_count)
// passes through unchanged — no double-truncation, and
// ComponentEvidenceTruncated reflects exactly what was already true at write
// time.
func TestDecodeSBOMAttestationAttachmentRowServesPostCapFactUnchanged(t *testing.T) {
	t.Parallel()

	components := []any{
		map[string]any{"component_id": "component-000", "name": "component-000", "fact_id": "fact-000"},
		map[string]any{"component_id": "component-001", "name": "component-001", "fact_id": "fact-001"},
		map[string]any{"component_id": "component-002", "name": "component-002", "fact_id": "fact-002"},
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"document_id":        "doc-post-cap-components",
		"attachment_status":  "attached_parse_only",
		"component_count":    3, // accurate: matches the 3 persisted (already-bounded) rows
		"component_evidence": components,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSBOMAttestationAttachmentRow("attachment-post-cap-components", "reported", payloadBytes)
	if err != nil {
		t.Fatalf("decodeSBOMAttestationAttachmentRow() error = %v", err)
	}
	if got, want := len(row.ComponentEvidence), 3; got != want {
		t.Fatalf("ComponentEvidence len = %d, want %d (already-bounded fact must not be further truncated)", got, want)
	}
	if got, want := row.ComponentCount, 3; got != want {
		t.Fatalf("ComponentCount = %d, want %d (accurate persisted count must be preserved)", got, want)
	}
	if row.ComponentEvidenceTruncated {
		t.Fatal("ComponentEvidenceTruncated = true, want false: a post-cap fact with count == len(rows) is not truncated")
	}
	for i, want := range []string{"component-000", "component-001", "component-002"} {
		if got := row.ComponentEvidence[i].ComponentID; got != want {
			t.Fatalf("ComponentEvidence[%d].ComponentID = %q, want %q (order must be preserved for an already-sorted fact)", i, got, want)
		}
	}
}
