// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"testing"
)

// TestRelationshipStoryProvenanceFamilies proves rows classify into
// value-flow, code-edge, correlation, and unsupported families with
// the matching truth state.
func TestRelationshipStoryProvenanceFamilies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		row        map[string]any
		family     string
		truthState string
	}{
		{
			name:       "value flow edge",
			row:        map[string]any{"type": "TAINT_FLOWS_TO", "confidence": 0.95},
			family:     relationshipStoryProvenanceValueFlowEdge,
			truthState: relationshipStoryTruthDerived,
		},
		{
			name:       "code edge",
			row:        map[string]any{"resolution_method": "ast", "confidence": 0.8},
			family:     relationshipStoryProvenanceCodeEdge,
			truthState: relationshipStoryTruthDerived,
		},
		{
			name:       "heuristic code edge",
			row:        map[string]any{"resolution_method": "semantic guess", "confidence": 0.8},
			family:     relationshipStoryProvenanceCodeEdge,
			truthState: relationshipStoryTruthHeuristic,
		},
		{
			name:       "correlation edge",
			row:        map[string]any{"evidence_type": "co-change", "confidence": 0.75},
			family:     relationshipStoryProvenanceCorrelationEdge,
			truthState: relationshipStoryTruthHeuristic,
		},
		{
			name:       "unsupported edge",
			row:        map[string]any{"confidence": 0.9},
			family:     relationshipStoryProvenanceUnsupported,
			truthState: relationshipStoryTruthUnsupported,
		},
		{
			name:       "confidenceless edge",
			row:        map[string]any{"resolution_method": "ast"},
			family:     relationshipStoryProvenanceCodeEdge,
			truthState: relationshipStoryTruthUnsupported,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provenance := RelationshipStoryProvenance(tc.row)
			if provenance["source_family"] != tc.family {
				t.Errorf("source_family = %v, want %q", provenance["source_family"], tc.family)
			}
			if provenance["truth_state"] != tc.truthState {
				t.Errorf("truth_state = %v, want %q", provenance["truth_state"], tc.truthState)
			}
		})
	}
}

// TestRelationshipStoryConfidenceTiers proves the numeric confidence
// maps onto the high/medium/low tiers and surfaces in the envelope.
func TestRelationshipStoryConfidenceTiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		confidence float64
		tier       string
	}{
		{0.95, relationshipStoryTierHigh},
		{0.70, relationshipStoryTierMedium},
		{0.10, relationshipStoryTierLow},
	}
	for _, tc := range cases {
		if got := relationshipStoryConfidenceTier(tc.confidence, true); got != tc.tier {
			t.Errorf("tier(%v) = %q, want %q", tc.confidence, got, tc.tier)
		}
	}
	if got := relationshipStoryConfidenceTier(0, false); got != relationshipStoryTierUnsupported {
		t.Errorf("confidenceless tier = %q, want unsupported", got)
	}

	provenance := RelationshipStoryProvenance(map[string]any{
		"confidence": 0.95, "resolution_method": "ast",
		"reason": "exact", "why_trail_json": `["a","b"]`,
	})
	if provenance["confidence_tier"] != relationshipStoryTierHigh {
		t.Errorf("confidence_tier = %v, want high", provenance["confidence_tier"])
	}
	if provenance["confidence"] != 0.95 {
		t.Errorf("confidence = %v, want 0.95", provenance["confidence"])
	}
	if provenance["reason"] != "exact" {
		t.Errorf("reason = %v, want exact", provenance["reason"])
	}
	trail, ok := provenance["why_trail"].([]any)
	if !ok || len(trail) != 2 {
		t.Errorf("why_trail must decode the JSON trail: %#v", provenance["why_trail"])
	}

	bare := RelationshipStoryProvenance(map[string]any{})
	if bare["confidence_state"] != relationshipStoryProvenanceUnsupported {
		t.Errorf("bare row confidence_state = %v, want unsupported", bare["confidence_state"])
	}
	if _, ok := bare["confidence"]; ok {
		t.Error("bare row must not fabricate a confidence value")
	}
}
