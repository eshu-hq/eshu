// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingRDSPostureNodeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingRDSPostureNodeWriter) WriteRDSPostureNodes(
	_ context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeScopeID = scopeID
	w.writeGenerationID = generationID
	w.writeEvidence = evidenceSource
	return nil
}

func (w *recordingRDSPostureNodeWriter) RetractRDSPostureNodes(
	_ context.Context,
	scopeIDs []string,
	_ string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func rdsPostureIntent() Intent {
	return Intent{
		IntentID:     "intent-rds-posture-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainRDSPostureMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func rdsPostureFixture() []facts.Envelope {
	instanceARN := "arn:aws:rds:us-east-1:111111111111:db:orders-writer"
	clusterARN := "arn:aws:rds:us-east-1:111111111111:cluster:orders"
	return []facts.Envelope{
		rdsResourceEnvelope(testRDSInstance, instanceARN, "orders-writer"),
		rdsResourceEnvelope(testRDSCluster, clusterARN, "orders"),
		rdsPostureEnvelope(testRDSInstance, instanceARN, "orders-writer", true),
		rdsPostureEnvelope(testRDSCluster, clusterARN, "orders", false),
	}
}

func TestRDSPostureMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := RDSPostureMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		NodeWriter:      &recordingRDSPostureNodeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := rdsPostureIntent()
	intent.Domain = DomainS3LogsToMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestRDSPostureMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := RDSPostureMaterializationHandler{
		NodeWriter:      &recordingRDSPostureNodeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), rdsPostureIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestRDSPostureMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := RDSPostureMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), rdsPostureIntent()); err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestRDSPostureMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingRDSPostureNodeWriter{}
	handler := RDSPostureMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: rdsPostureFixture()},
		NodeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), rdsPostureIntent())
	if err == nil {
		t.Fatal("expected a retryable error while canonical CloudResource nodes are not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before readiness: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestRDSPostureMaterializationProjectsNodeProperties(t *testing.T) {
	t.Parallel()

	writer := &recordingRDSPostureNodeWriter{}
	handler := RDSPostureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: rdsPostureFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), rdsPostureIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 2 {
		t.Fatalf("written posture rows = %d, want 2", len(writer.writtenRows))
	}
	if writer.writeScopeID != "scope-1" || writer.writeGenerationID != "gen-1" {
		t.Fatalf("write scope/generation = %q/%q, want scope-1/gen-1", writer.writeScopeID, writer.writeGenerationID)
	}
	if writer.writeEvidence != rdsPostureEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, rdsPostureEvidenceSource)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 (prior generation exists)", writer.retractCalls)
	}
	if writer.retractEvidence != rdsPostureEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, rdsPostureEvidenceSource)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
}

func TestRDSPostureMaterializationFirstGenerationSkipsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingRDSPostureNodeWriter{}
	handler := RDSPostureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: rdsPostureFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), rdsPostureIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 for first generation", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
}
