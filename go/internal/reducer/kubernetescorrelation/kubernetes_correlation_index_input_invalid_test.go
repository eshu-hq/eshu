// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetescorrelation

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestKubernetesRelationshipQuarantinesMissingRelationshipType proves the same
// per-fact isolation and accuracy contract for the
// kubernetes_live.relationship kind: a directed edge fact missing its
// required relationship_type key is QUARANTINED as an input_invalid
// dead-letter rather than silently producing an edge with a blank type or
// missing endpoint.
func TestKubernetesRelationshipQuarantinesMissingRelationshipType(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactKind: facts.KubernetesRelationshipFactKind,
		FactID:   "fact-rel-malformed",
		Payload: map[string]any{
			// "relationship_type" intentionally absent.
			"from_object_id": "object-a",
			"to_object_id":   "object-b",
			"cluster_id":     "prod-eks",
		},
	}

	index, quarantined, err := buildKubernetesCorrelationIndex([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("buildKubernetesCorrelationIndex() error = %v, want nil (a missing required field is a quarantine, not a fatal error)", err)
	}
	if len(index.identityEdges) != 0 {
		t.Fatalf("identityEdges = %v, want 0; a relationship fact missing relationship_type must not produce an identity edge", index.identityEdges)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %v, want exactly 1; the missing-relationship_type fact must be recorded as one input_invalid quarantine", quarantined)
	}
	if quarantined[0].Field != "relationship_type" {
		t.Fatalf("quarantined[0].Field = %q, want %q", quarantined[0].Field, "relationship_type")
	}
}

// TestKubernetesWarningQuarantinesMissingReason proves the same contract for
// the kubernetes_live.warning kind: a warning fact missing its required
// reason key is QUARANTINED as an input_invalid dead-letter rather than
// silently contributing an empty-string warning.
func TestKubernetesWarningQuarantinesMissingReason(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactKind: facts.KubernetesWarningFactKind,
		FactID:   "fact-warn-malformed",
		Payload: map[string]any{
			// "reason" intentionally absent.
			"cluster_id":     "prod-eks",
			"resource_scope": "apps/v1/deployments",
		},
	}

	index, quarantined, err := buildKubernetesCorrelationIndex([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("buildKubernetesCorrelationIndex() error = %v, want nil (a missing required field is a quarantine, not a fatal error)", err)
	}
	if len(index.warnings) != 0 {
		t.Fatalf("warnings = %v, want 0; a warning fact missing reason must not be recorded", index.warnings)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %v, want exactly 1; the missing-reason fact must be recorded as one input_invalid quarantine", quarantined)
	}
	if quarantined[0].Field != "reason" {
		t.Fatalf("quarantined[0].Field = %q, want %q", quarantined[0].Field, "reason")
	}
}
