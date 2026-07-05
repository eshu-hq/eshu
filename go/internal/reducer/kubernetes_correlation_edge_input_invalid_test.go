// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestKubernetesCorrelationMaterializationQuarantinesMalformedFact proves the
// edge materialization path propagates a per-fact input_invalid quarantine the
// same way the node path does (issue #388 PR3 / Contract System v1 wave 4b). A
// malformed kubernetes_live fact (missing a required field) is quarantined and
// counted on Result.SubSignals["input_invalid_facts"], while a valid
// workload+image pair in the same batch still materializes its RUNS_IMAGE edge.
//
// Before the fix, ExtractKubernetesCorrelationEdgeRows called
// BuildKubernetesCorrelationDecisions and discarded both the error and the
// quarantine, so the malformed fact was invisible to the operator (SubSignals
// stayed nil, the counter never incremented) — an inconsistency with the node
// path that this test locks out.
func TestKubernetesCorrelationMaterializationQuarantinesMalformedFact(t *testing.T) {
	t.Parallel()

	// A relationship fact missing its required relationship_type key: an
	// input_invalid quarantine (a per-fact dead-letter), not a fatal error.
	malformed := facts.Envelope{
		FactID:   "rel-malformed",
		FactKind: facts.KubernetesRelationshipFactKind,
		Payload: map[string]any{
			// "relationship_type" intentionally absent.
			"from_object_id": "k8s://a",
			"to_object_id":   "k8s://b",
			"cluster_id":     testK8sCluster,
		},
	}
	// The canonical valid exact-digest workload+manifest pair that must still
	// materialize its RUNS_IMAGE edge despite the poisoned sibling.
	envelopes := append(exactDigestEdgeFixture(), malformed)

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: envelopes},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error %v; a malformed fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-relationship_type fact must be recorded as one input_invalid quarantine", got)
	}
	// The valid exact-digest edge must still materialize: isolation means a
	// poisoned sibling never suppresses valid graph truth.
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1; the valid RUNS_IMAGE edge must still project despite the quarantined fact", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written RUNS_IMAGE rows = %d, want exactly the one valid edge", len(writer.writtenRows))
	}
}

// fatalDecodeEdgeFactLoader returns scope facts on the kind-scoped load and
// nothing on the active-source load, but wraps a fact whose schema major is
// unsupported (a FATAL decode error, not a per-fact input_invalid quarantine).
type fatalDecodeEdgeFactLoader struct {
	scopeFacts []facts.Envelope
}

func (l *fatalDecodeEdgeFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *fatalDecodeEdgeFactLoader) ListFactsByKind(_ context.Context, _ string, _ string, _ []string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *fatalDecodeEdgeFactLoader) ListActiveContainerImageIdentityFacts(context.Context) ([]facts.Envelope, error) {
	return nil, nil
}

// TestKubernetesCorrelationMaterializationFatalDecodeDoesNotRetract proves the
// edge handler returns a fatal extraction error WITHOUT retracting prior edges.
// A fatal decode (an unsupported schema major — version skew, not a malformed
// individual payload) must fail the whole intent for durable triage; it must
// NOT reach the retract call, because retracting on a fatal error would delete
// the prior generation's valid RUNS_IMAGE edges and then write nothing —
// silent edge loss on a transient/version-skew condition.
//
// Before the fix, ExtractKubernetesCorrelationEdgeRows swallowed the error and
// returned empty decisions, so the handler proceeded to Retract (deleting prior
// edges) and wrote nothing. This test asserts the error surfaces and Retract is
// never called.
func TestKubernetesCorrelationMaterializationFatalDecodeDoesNotRetract(t *testing.T) {
	t.Parallel()

	// A pod_template fact whose schema major (2) has no decode path: a fatal
	// ErrUnsupportedSchemaMajor, excluded from the per-fact quarantine path.
	fatal := facts.Envelope{
		FactID:        "pod-unsupported-major",
		FactKind:      facts.KubernetesPodTemplateFactKind,
		SchemaVersion: "2.0.0",
		Payload: map[string]any{
			"object_id": "k8s://unsupported",
		},
	}

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &fatalDecodeEdgeFactLoader{scopeFacts: []facts.Envelope{fatal}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	_, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	if err == nil {
		t.Fatal("Handle returned nil; a fatal unsupported-schema-major decode must fail the whole intent")
	}
	if !errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("Handle error = %v, want it to wrap factschema.ErrUnsupportedSchemaMajor", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0; a fatal extraction error must NOT retract prior edges (would be silent edge loss)", writer.retractCalls)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0; a fatal error must not write", writer.writeCalls)
	}
}
