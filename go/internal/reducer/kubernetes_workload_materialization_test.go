// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingKubernetesWorkloadNodeWriter captures the rows handed to the node
// writer so tests can assert on the exact materialization request.
type recordingKubernetesWorkloadNodeWriter struct {
	calls          int
	rows           []map[string]any
	evidenceSource string
	err            error
}

func (w *recordingKubernetesWorkloadNodeWriter) WriteKubernetesWorkloadNodes(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.calls++
	w.rows = append(w.rows, rows...)
	w.evidenceSource = evidenceSource
	return w.err
}

func kubernetesPodTemplateEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.KubernetesPodTemplateFactKind,
		FactID:   "fact-" + anyToString(payload["object_id"]),
		Payload:  payload,
	}
}

func samplePodTemplatePayload(objectID, name string) map[string]any {
	return map[string]any{
		"object_id":              objectID,
		"cluster_id":             "prod-eks",
		"namespace":              "payments",
		"name":                   name,
		"uid":                    "11111111-2222-3333-4444-555555555555",
		"group_version_resource": "apps/v1/deployments",
		"service_account":        "checkout-sa",
		"image_refs":             []any{"registry.example.com/checkout@sha256:abc"},
		"selector":               map[string]string{"app": "checkout"},
		"correlation_anchors":    []any{objectID, "registry.example.com/checkout@sha256:abc"},
	}
}

func TestKubernetesWorkloadMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingKubernetesWorkloadNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestKubernetesWorkloadMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := KubernetesWorkloadMaterializationHandler{
		NodeWriter: &recordingKubernetesWorkloadNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestKubernetesWorkloadMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestExtractKubernetesWorkloadNodeRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	rows, quarantined, err := ExtractKubernetesWorkloadNodeRows(nil)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if quarantined != nil {
		t.Fatalf("quarantined = %v, want nil", quarantined)
	}
}

func TestExtractKubernetesWorkloadNodeRowsBuildsObjectIDIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
	}

	rows, _, err := ExtractKubernetesWorkloadNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got := anyToString(rows[0]["uid"]); got != "object-a" {
		t.Fatalf("uid = %q, want object-a (the collector-emitted object_id)", got)
	}
	if got := anyToString(rows[0]["workload_uid"]); got != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("workload_uid = %q, want the raw k8s metadata.uid as a property only", got)
	}
	if got := anyToString(rows[0]["cluster_id"]); got != "prod-eks" {
		t.Fatalf("cluster_id = %q", got)
	}
	if got := anyToString(rows[0]["namespace"]); got != "payments" {
		t.Fatalf("namespace = %q", got)
	}
	images, ok := rows[0]["image_refs"].([]string)
	if !ok {
		t.Fatalf("image_refs type = %T, want []string", rows[0]["image_refs"])
	}
	if len(images) != 1 || images[0] != "registry.example.com/checkout@sha256:abc" {
		t.Fatalf("image_refs = %v", images)
	}
	selector, ok := rows[0]["selector"].([]string)
	if !ok {
		t.Fatalf("selector type = %T, want []string", rows[0]["selector"])
	}
	if len(selector) != 1 || selector[0] != "app=checkout" {
		t.Fatalf("selector = %v, want [app=checkout]", selector)
	}
}

func TestExtractKubernetesWorkloadNodeRowsSkipsNonPodTemplateFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: facts.KubernetesRelationshipFactKind, Payload: map[string]any{"from_object_id": "ignored"}},
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
	}

	rows, _, err := ExtractKubernetesWorkloadNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (relationship facts must be skipped)", len(rows))
	}
}

func TestExtractKubernetesWorkloadNodeRowsRequiresObjectID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		// Missing object_id: quarantined as an input_invalid dead-letter, not a
		// materializable node.
		kubernetesPodTemplateEnvelope(map[string]any{
			"cluster_id": "prod-eks",
			"namespace":  "payments",
			"name":       "checkout",
		}),
	}

	rows, quarantined, err := ExtractKubernetesWorkloadNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for missing object_id", len(rows))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 for missing object_id", len(quarantined))
	}
	if quarantined[0].field != "object_id" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "object_id")
	}
}

func TestExtractKubernetesWorkloadNodeRowsDeduplicatesByObjectID(t *testing.T) {
	t.Parallel()

	payload := samplePodTemplatePayload("object-a", "checkout")
	envelopes := []facts.Envelope{
		kubernetesPodTemplateEnvelope(payload),
		kubernetesPodTemplateEnvelope(payload),
	}

	rows, _, err := ExtractKubernetesWorkloadNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (duplicate facts must converge on one node)", len(rows))
	}
}

func TestExtractKubernetesWorkloadNodeRowsSkipsTombstone(t *testing.T) {
	t.Parallel()

	tombstone := kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-gone", "deleted"))
	tombstone.IsTombstone = true
	envelopes := []facts.Envelope{
		tombstone,
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
	}

	rows, _, err := ExtractKubernetesWorkloadNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (a tombstoned workload no longer runs and must not materialize)", len(rows))
	}
	if got := anyToString(rows[0]["uid"]); got != "object-a" {
		t.Fatalf("uid = %q, want object-a", got)
	}
}

func TestExtractKubernetesWorkloadNodeRowsSortedByUID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-c", "c")),
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "a")),
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-b", "b")),
	}

	rows, _, err := ExtractKubernetesWorkloadNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractKubernetesWorkloadNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	want := []string{"object-a", "object-b", "object-c"}
	for i, uid := range want {
		if got := anyToString(rows[i]["uid"]); got != uid {
			t.Fatalf("rows[%d].uid = %q, want %q (deterministic byte-stable order)", i, got, uid)
		}
	}
}

func TestKubernetesWorkloadMaterializationHandleWritesNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesWorkloadNodeWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-b", "ledger")),
	}}

	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	if len(writer.rows) != 2 {
		t.Fatalf("len(writer.rows) = %d, want 2", len(writer.rows))
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
	if writer.evidenceSource != kubernetesWorkloadEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", writer.evidenceSource, kubernetesWorkloadEvidenceSource)
	}
}

func TestKubernetesWorkloadMaterializationHandleNoFactsIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesWorkloadNodeWriter{}
	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (no facts must not write)", writer.calls)
	}
}

func TestKubernetesWorkloadMaterializationHandlePublishesCanonicalNodesCommittedPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
	}}
	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     &recordingKubernetesWorkloadNodeWriter{},
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (the edge slice gates on this readiness phase)", len(publisher.calls))
	}
	rows := publisher.calls[0]
	if len(rows) != 1 {
		t.Fatalf("published rows = %d, want 1", len(rows))
	}
	if got, want := rows[0].Key.Keyspace, GraphProjectionKeyspaceKubernetesWorkloadUID; got != want {
		t.Fatalf("keyspace = %q, want %q", got, want)
	}
	if got, want := rows[0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
	if got, want := rows[0].Key.ScopeID, "scope-1"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
	if got, want := rows[0].Key.GenerationID, "gen-1"; got != want {
		t.Fatalf("generation = %q, want %q", got, want)
	}
}

func TestKubernetesWorkloadMaterializationHandlePublishesPhaseOnEmptyGeneration(t *testing.T) {
	t.Parallel()

	// A generation that observed zero materializable workloads must still publish
	// the canonical-nodes-committed phase, otherwise the later edge slice never
	// observes that the node stage completed and blocks forever on the gate.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingKubernetesWorkloadNodeWriter{}
	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader:     &stubFactLoader{},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (no facts must not write)", writer.calls)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (empty generation must still unblock the edge slice)", len(publisher.calls))
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceKubernetesWorkloadUID; got != want {
		t.Fatalf("keyspace = %q, want %q", got, want)
	}
}

func TestKubernetesWorkloadMaterializationHandleDoesNotPublishPhaseOnWriteFailure(t *testing.T) {
	t.Parallel()

	// Publishing the readiness gate after a failed node write would let the edge
	// slice resolve edges against nodes that never committed. The phase must only
	// be published once the canonical node write succeeds.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingKubernetesWorkloadNodeWriter{err: errors.New("graph backend unavailable")}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
	}}
	handler := KubernetesWorkloadMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err == nil {
		t.Fatal("expected error when node write fails")
	}

	if len(publisher.calls) != 0 {
		t.Fatalf("publisher.calls = %d, want 0 (no readiness gate after a failed write)", len(publisher.calls))
	}
}
