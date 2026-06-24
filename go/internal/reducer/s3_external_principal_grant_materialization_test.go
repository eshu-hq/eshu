// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingS3ExternalPrincipalGrantWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingS3ExternalPrincipalGrantWriter) WriteS3ExternalPrincipalGrants(
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

func (w *recordingS3ExternalPrincipalGrantWriter) RetractS3ExternalPrincipalGrants(
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

func s3ExternalPrincipalGrantIntent() Intent {
	return Intent{
		IntentID:     "intent-s3-external-principal-grant-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainS3ExternalPrincipalGrantMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestS3ExternalPrincipalGrantMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingS3ExternalPrincipalGrantWriter{}
	handler := S3ExternalPrincipalGrantMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: s3ExternalPrincipalGrantFacts()},
		GrantWriter:     writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), s3ExternalPrincipalGrantIntent())
	if err == nil {
		t.Fatal("expected a retryable error while canonical nodes phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before S3 source nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestS3ExternalPrincipalGrantMaterializationProjectsGrantEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingS3ExternalPrincipalGrantWriter{}
	handler := S3ExternalPrincipalGrantMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: s3ExternalPrincipalGrantFacts()},
		GrantWriter:          writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), s3ExternalPrincipalGrantIntent())
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
		t.Fatalf("written grant rows = %d, want 2 exact grants", len(writer.writtenRows))
	}
	if writer.writeEvidence != s3ExternalPrincipalGrantEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, s3ExternalPrincipalGrantEvidenceSource)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 because a prior generation exists", writer.retractCalls)
	}
}

func TestS3ExternalPrincipalGrantMaterializationFirstGenerationSkipsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingS3ExternalPrincipalGrantWriter{}
	handler := S3ExternalPrincipalGrantMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: s3ExternalPrincipalGrantFacts()},
		GrantWriter:          writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), s3ExternalPrincipalGrantIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on first generation", writer.retractCalls)
	}
}

func s3ExternalPrincipalGrantFacts() []facts.Envelope {
	return []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-artifacts"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "reports"),
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "orders-artifacts", "aws_account", "999988887777", "cross_account"),
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "reports", "public", "*", "public"),
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "orders-artifacts", "unsupported", "AWS", "unsupported_principal"),
	}
}
