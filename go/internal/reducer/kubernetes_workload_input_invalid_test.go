// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestKubernetesWorkloadMaterializationQuarantinesMissingObjectID is the
// flagship regression test for the kubernetes_live family's typed-decode
// migration (Contract System v1 §3.2, mirroring
// TestGCPResourceMaterializationQuarantinesMissingFullResourceName). It proves
// the accuracy guarantee the migration exists to protect AND the per-fact
// isolation contract: a kubernetes_live.pod_template fact missing its required
// object_id key is QUARANTINED as a visible input_invalid dead-letter — never
// silently producing an empty-string KubernetesWorkload node identity — while
// every VALID fact in the same batch still projects and the handler succeeds
// so one malformed fact never stalls the scope generation.
//
// Before the migration this behavior was impossible: kubernetesWorkloadNodeRow
// read object_id with payloadString, which returns "" for the absent key, and
// the "" == "" check made the row look correctly rejected only by luck (the
// pre-typing code special-cased objectID == ""); a rename to a different
// required key would NOT have been caught the same way. After the migration
// ExtractKubernetesWorkloadNodeRows decodes each kubernetes_live.pod_template
// fact through factschema.DecodeKubernetesLivePodTemplate; the malformed fact
// yields a classified *factschema.DecodeError that partitionDecodeFailures
// routes to a per-fact quarantine. The handler records it (metric + structured
// log + the input_invalid_facts SubSignal) and continues, so the batch's
// valid workload still materializes its node.
func TestKubernetesWorkloadMaterializationQuarantinesMissingObjectID(t *testing.T) {
	t.Parallel()

	// A pod-template fact whose required object_id key is ABSENT (not merely
	// empty): the exact malformed input the accuracy guarantee names.
	// Everything else is present so the ONLY reason to quarantine the fact is
	// the missing required field.
	malformed := facts.Envelope{
		FactKind: facts.KubernetesPodTemplateFactKind,
		FactID:   "fact-malformed",
		Payload: map[string]any{
			// "object_id" intentionally absent.
			"cluster_id":             "prod-eks",
			"namespace":              "payments",
			"name":                   "checkout-bad",
			"uid":                    "11111111-2222-3333-4444-555555555555",
			"group_version_resource": "apps/v1/deployments",
			"service_account":        "checkout-sa",
		},
	}
	// A fully valid, independent pod template that must still project despite
	// the malformed fact sharing the batch. This is the isolation half of the
	// contract: valid facts are unaffected by a poisoned sibling.
	valid := kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-good", "checkout-good"))

	writer := &recordingKubernetesWorkloadNodeWriter{}
	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{malformed, valid}},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
	})
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed kubernetes_live.pod_template fact must be quarantined per-fact, not fail the whole intent", err)
	}

	// The malformed fact must be counted as an input_invalid quarantine in the
	// Result SubSignals so the operator sees it on the per-intent signal (each
	// quarantined fact is also on the eshu_dp_reducer_input_invalid_facts_total
	// counter and a structured error log).
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-object_id fact must be recorded as one input_invalid quarantine", got)
	}

	// The batch's VALID workload must still materialize its node: isolation
	// means a poisoned sibling never suppresses valid graph truth.
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1; the valid workload must still project despite the quarantined fact", writer.calls)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("len(writer.rows) = %d, want 1; exactly the one valid node must be written", len(writer.rows))
	}
	if writer.rows[0]["uid"] != "object-good" {
		t.Fatalf("written node uid = %v, want %q", writer.rows[0]["uid"], "object-good")
	}

	// No node may be written under an empty-string uid — the accuracy
	// guarantee this migration exists to enforce.
	for _, row := range writer.rows {
		if row["uid"] == "" {
			t.Fatal("written node references an empty-string uid; a quarantined fact must never produce graph identity")
		}
	}
}

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
	if quarantined[0].field != "relationship_type" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "relationship_type")
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
	if quarantined[0].field != "reason" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "reason")
	}
}
