// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingS3LogsToEdgeWriter captures LOGS_TO edge writes and retracts so tests
// can assert the exact materialization request.
type recordingS3LogsToEdgeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
	writeErr          error
	retractErr        error
}

func (w *recordingS3LogsToEdgeWriter) WriteS3LogsToEdges(
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
	return w.writeErr
}

func (w *recordingS3LogsToEdgeWriter) RetractS3LogsToEdges(
	_ context.Context,
	scopeIDs []string,
	_ string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return w.retractErr
}

func s3LogsToIntent() Intent {
	return Intent{
		IntentID:     "intent-s3-logs-to-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainS3LogsToMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// s3LogsToFixture is two source buckets (orders, payments) each logging to a
// scanned central log bucket, plus one bucket whose log target was not scanned
// (cross-account) and must be skipped, not written.
func s3LogsToFixture() []facts.Envelope {
	const acct = "111111111111"
	const region = "us-east-1"
	return []facts.Envelope{
		s3BucketResourceEnvelope(acct, region, "orders"),
		s3BucketResourceEnvelope(acct, region, "payments"),
		s3BucketResourceEnvelope(acct, region, "central-logs"),
		s3BucketResourceEnvelope(acct, region, "isolated"),
		s3PostureEnvelope(acct, region, "orders", "central-logs"),
		s3PostureEnvelope(acct, region, "payments", "central-logs"),
		s3PostureEnvelope(acct, region, "isolated", "offsite-logs"), // target not scanned
	}
}

func TestS3LogsToMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := S3LogsToMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingS3LogsToEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := s3LogsToIntent()
	intent.Domain = DomainAWSRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestS3LogsToMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := S3LogsToMaterializationHandler{
		EdgeWriter:      &recordingS3LogsToEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), s3LogsToIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestS3LogsToMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := S3LogsToMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), s3LogsToIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestS3LogsToMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingS3LogsToEdgeWriter{}
	handler := S3LogsToMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: s3LogsToFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false), // #805 PR1 nodes phase not yet committed
	}

	_, err := handler.Handle(context.Background(), s3LogsToIntent())
	if err == nil {
		t.Fatal("expected a retryable error while canonical nodes phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestS3LogsToMaterializationProjectsLogsToEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingS3LogsToEdgeWriter{}
	handler := S3LogsToMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: s3LogsToFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), s3LogsToIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	// orders + payments -> central-logs; isolated -> offsite-logs skipped.
	if len(writer.writtenRows) != 2 {
		t.Fatalf("written LOGS_TO rows = %d, want 2 (isolated target unscanned)", len(writer.writtenRows))
	}
	if writer.writeEvidence != s3LogsToEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, s3LogsToEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" {
		t.Fatalf("write scope id = %q, want scope-1", writer.writeScopeID)
	}
	if writer.writeGenerationID != "gen-1" {
		t.Fatalf("write generation id = %q, want gen-1", writer.writeGenerationID)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 (prior generation exists)", writer.retractCalls)
	}
	if writer.retractEvidence != s3LogsToEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, s3LogsToEvidenceSource)
	}
}

func TestS3LogsToMaterializationIdempotentOnReprojection(t *testing.T) {
	t.Parallel()

	writer := &recordingS3LogsToEdgeWriter{}
	handler := S3LogsToMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: s3LogsToFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	first, err := handler.Handle(context.Background(), s3LogsToIntent())
	if err != nil {
		t.Fatalf("first Handle error: %v", err)
	}
	second, err := handler.Handle(context.Background(), s3LogsToIntent())
	if err != nil {
		t.Fatalf("second Handle error: %v", err)
	}
	if first.CanonicalWrites != second.CanonicalWrites {
		t.Fatalf("reprojection changed write count: first=%d second=%d", first.CanonicalWrites, second.CanonicalWrites)
	}
	if writer.writeCalls != 2 {
		t.Fatalf("writeCalls = %d, want 2 (one per reprojection)", writer.writeCalls)
	}
}

func TestS3LogsToMaterializationFirstGenerationSkipsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingS3LogsToEdgeWriter{}
	handler := S3LogsToMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: s3LogsToFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), s3LogsToIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	// No prior generation and first attempt: the retract is skipped.
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 (no prior generation, first attempt)", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
}

func TestS3LogsToMaterializationEmptyGenerationNoWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingS3LogsToEdgeWriter{}
	handler := S3LogsToMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	result, err := handler.Handle(context.Background(), s3LogsToIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for empty generation", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
}
