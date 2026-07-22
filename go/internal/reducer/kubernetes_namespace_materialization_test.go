// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingKubernetesNamespaceNodeWriter captures the rows handed to the
// node writer so tests can assert on the exact materialization request.
type recordingKubernetesNamespaceNodeWriter struct {
	calls               int
	rows                []map[string]any
	evidenceSource      string
	err                 error
	retractCalls        int
	retractClusterID    string
	retractGenerationID string
	retractErr          error
	events              []string
}

func (w *recordingKubernetesNamespaceNodeWriter) RetractStaleKubernetesNamespaceNodes(
	_ context.Context,
	clusterID string,
	generationID string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.events = append(w.events, "retract")
	w.retractClusterID = clusterID
	w.retractGenerationID = generationID
	w.evidenceSource = evidenceSource
	return w.retractErr
}

func (w *recordingKubernetesNamespaceNodeWriter) WriteKubernetesNamespaceNodes(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.calls++
	w.events = append(w.events, "write")
	w.rows = append(w.rows, rows...)
	w.evidenceSource = evidenceSource
	return w.err
}

func kubernetesNamespaceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.KubernetesNamespaceFactKind,
		FactID:   "fact-" + anyToString(payload["object_id"]),
		Payload:  payload,
	}
}

func sampleNamespacePayload(objectID, namespaceName string, labels map[string]string) map[string]any {
	return map[string]any{
		"object_id":           objectID,
		"cluster_id":          "prod-eks",
		"namespace":           namespaceName,
		"labels":              labels,
		"correlation_anchors": []any{objectID},
	}
}

func TestKubernetesNamespaceMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingKubernetesNamespaceNodeWriter{},
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

func TestKubernetesNamespaceMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := KubernetesNamespaceMaterializationHandler{
		NodeWriter: &recordingKubernetesNamespaceNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesNamespaceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestKubernetesNamespaceMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesNamespaceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestExtractKubernetesNamespaceNodeRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	rows, boundCount, quarantined, err := ExtractKubernetesNamespaceNodeRows(nil)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if boundCount != 0 {
		t.Fatalf("boundCount = %d, want 0", boundCount)
	}
	if quarantined != nil {
		t.Fatalf("quarantined = %v, want nil", quarantined)
	}
}

// TestExtractKubernetesNamespaceNodeRowsAliasedLabelBindsEnvironment is the
// POSITIVE case: a namespace whose "environment" label carries an alias
// ("production") resolves through environment.Canonical to "prod", is
// classified StateBound with EvidenceClassNamespaceLabel, and its row's
// "environment" property is non-empty (issue #5434).
func TestExtractKubernetesNamespaceNodeRowsAliasedLabelBindsEnvironment(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload(
			"object-payments-prod", "payments-prod",
			map[string]string{"environment": "production", "team": "payments"},
		)),
	}

	rows, boundCount, quarantined, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %v, want none", quarantined)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if boundCount != 1 {
		t.Fatalf("boundCount = %d, want 1", boundCount)
	}
	row := rows[0]
	if got := row["environment"]; got != "prod" {
		t.Fatalf("row[environment] = %v, want %q (alias canonicalized)", got, "prod")
	}
	if got := row["environment_state"]; got != string(environment.StateBound) {
		t.Fatalf("row[environment_state] = %v, want %q", got, environment.StateBound)
	}
	if got := row["evidence_class"]; got != string(environment.EvidenceClassNamespaceLabel) {
		t.Fatalf("row[evidence_class] = %v, want %q", got, environment.EvidenceClassNamespaceLabel)
	}
}

// TestExtractKubernetesNamespaceNodeRowsNonAliasedLabelStaysUnbound is the
// NEGATIVE regression lock for issue #5434: a namespace with labels that do
// NOT declare a recognized environment (a namespace-like string is not a
// real environment) must classify StateEnvironmentUnbound with an empty
// "environment" property. If namespaceEnvironmentFromLabels's IsKnownToken
// guard were removed -- for example if any non-empty label value were
// trusted -- this test fails because row[environment] would become
// non-empty for a value like "misc-team" that is not one of the 12 known
// environment tokens.
func TestExtractKubernetesNamespaceNodeRowsNonAliasedLabelStaysUnbound(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload(
			"object-misc", "misc",
			map[string]string{"environment": "misc-team", "team": "platform"},
		)),
	}

	rows, boundCount, quarantined, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %v, want none", quarantined)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if boundCount != 0 {
		t.Fatalf("boundCount = %d, want 0", boundCount)
	}
	row := rows[0]
	if got := row["environment"]; got != "" {
		t.Fatalf("row[environment] = %v, want empty (unrecognized label value must never bind)", got)
	}
	if got := row["environment_state"]; got != string(environment.StateEnvironmentUnbound) {
		t.Fatalf("row[environment_state] = %v, want %q", got, environment.StateEnvironmentUnbound)
	}
	if got := row["evidence_class"]; got != "" {
		t.Fatalf("row[evidence_class] = %v, want empty for an unbound namespace", got)
	}
}

// TestExtractKubernetesNamespaceNodeRowsNoLabelsStaysUnbound proves a
// namespace with no labels at all (nil map) stays unbound rather than
// panicking or fabricating a default environment.
func TestExtractKubernetesNamespaceNodeRowsNoLabelsStaysUnbound(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-default", "default", nil)),
	}

	rows, boundCount, _, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if boundCount != 0 {
		t.Fatalf("boundCount = %d, want 0", boundCount)
	}
	if got := rows[0]["environment"]; got != "" {
		t.Fatalf("row[environment] = %v, want empty", got)
	}
}

// TestExtractKubernetesNamespaceNodeRowsRecommendedLabelKeyBinds proves the
// second documented label key (app.kubernetes.io/environment) also binds
// when "environment" is absent.
func TestExtractKubernetesNamespaceNodeRowsRecommendedLabelKeyBinds(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload(
			"object-checkout-stage", "checkout-stage",
			map[string]string{"app.kubernetes.io/environment": "staging"},
		)),
	}

	rows, boundCount, _, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if boundCount != 1 {
		t.Fatalf("boundCount = %d, want 1", boundCount)
	}
	if got := rows[0]["environment"]; got != "stage" {
		t.Fatalf("row[environment] = %v, want %q (staging alias canonicalized)", got, "stage")
	}
}

func TestExtractKubernetesNamespaceNodeRowsSkipsNonNamespaceFacts(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		{FactKind: facts.KubernetesPodTemplateFactKind, Payload: map[string]any{"object_id": "pod-a"}},
	}
	rows, _, quarantined, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if quarantined != nil {
		t.Fatalf("quarantined = %v, want nil", quarantined)
	}
}

func TestExtractKubernetesNamespaceNodeRowsRequiresObjectID(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(map[string]any{
			"cluster_id": "prod-eks",
			"namespace":  "payments-prod",
		}),
	}
	rows, _, quarantined, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 input_invalid quarantine", len(quarantined))
	}
}

func TestExtractKubernetesNamespaceNodeRowsDeduplicatesByObjectID(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-a", "payments-prod", map[string]string{"environment": "prod"})),
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-a", "payments-prod", map[string]string{"environment": "prod"})),
	}
	rows, _, _, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (deduplicated by object_id)", len(rows))
	}
}

func TestExtractKubernetesNamespaceNodeRowsSkipsTombstone(t *testing.T) {
	t.Parallel()

	env := kubernetesNamespaceEnvelope(sampleNamespacePayload("object-a", "payments-prod", nil))
	env.IsTombstone = true
	rows, _, _, err := ExtractKubernetesNamespaceNodeRows([]facts.Envelope{env})
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for tombstoned namespace", len(rows))
	}
}

// TestExtractKubernetesNamespaceNodeRowsIdempotentAcrossRuns proves the
// extraction is deterministic and idempotent: running it twice against the
// same input envelopes produces byte-identical rows in the same order, which
// is what makes the (cluster_id, namespace)-keyed MERGE write safe to replay.
func TestExtractKubernetesNamespaceNodeRowsIdempotentAcrossRuns(t *testing.T) {
	t.Parallel()

	envs := []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-b", "misc", map[string]string{"team": "platform"})),
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-a", "payments-prod", map[string]string{"environment": "prod"})),
	}

	first, firstBound, _, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() first run error = %v", err)
	}
	second, secondBound, _, err := ExtractKubernetesNamespaceNodeRows(envs)
	if err != nil {
		t.Fatalf("ExtractKubernetesNamespaceNodeRows() second run error = %v", err)
	}
	if firstBound != secondBound {
		t.Fatalf("boundCount not idempotent: %d vs %d", firstBound, secondBound)
	}
	if len(first) != len(second) || len(first) != 2 {
		t.Fatalf("row counts differ or unexpected: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i]["uid"] != second[i]["uid"] {
			t.Fatalf("row[%d] uid not idempotent: %v vs %v", i, first[i]["uid"], second[i]["uid"])
		}
		if first[i]["environment"] != second[i]["environment"] {
			t.Fatalf("row[%d] environment not idempotent: %v vs %v", i, first[i]["environment"], second[i]["environment"])
		}
	}
	// Sorted by uid ("object-a" < "object-b").
	if first[0]["uid"] != "object-a" || first[1]["uid"] != "object-b" {
		t.Fatalf("rows not sorted by uid: %v, %v", first[0]["uid"], first[1]["uid"])
	}
}

func TestKubernetesNamespaceMaterializationHandleWritesNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-a", "payments-prod", map[string]string{"environment": "prod"})),
		kubernetesNamespaceEnvelope(sampleNamespacePayload("object-b", "misc", map[string]string{"team": "platform"})),
	}}

	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesNamespaceMaterialization,
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
	if writer.retractCalls != 0 {
		t.Fatalf("writer.retractCalls = %d, want 0 without a complete-snapshot marker", writer.retractCalls)
	}
	if len(writer.rows) != 2 {
		t.Fatalf("len(writer.rows) = %d, want 2", len(writer.rows))
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
	if writer.evidenceSource != kubernetesNamespaceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", writer.evidenceSource, kubernetesNamespaceEvidenceSource)
	}
}

func TestKubernetesNamespaceMaterializationHandleNoFactsIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{}
	loader := &stubFactLoader{}

	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesNamespaceMaterialization,
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
		t.Fatalf("writer.calls = %d, want 0 for an empty generation", writer.calls)
	}
}
