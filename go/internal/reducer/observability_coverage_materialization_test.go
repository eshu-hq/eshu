// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingObservabilityCoverageEdgeWriter captures COVERS edge writes and
// retracts so tests can assert the exact materialization request.
type recordingObservabilityCoverageEdgeWriter struct {
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

	// anchored-delete method
	retractByUIDsCalls    int
	retractByUIDsUids     []string
	retractByUIDsScopes   []string
	retractByUIDsEvidence string
	retractByUIDsErr      error
}

func (w *recordingObservabilityCoverageEdgeWriter) WriteObservabilityCoverageEdges(
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

func (w *recordingObservabilityCoverageEdgeWriter) RetractObservabilityCoverageEdges(
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

func (w *recordingObservabilityCoverageEdgeWriter) RetractObservabilityCoverageEdgesByUIDs(
	_ context.Context,
	sourceUIDs []string,
	scopeIDs []string,
	evidenceSource string,
) error {
	w.retractByUIDsCalls++
	w.retractByUIDsUids = append(w.retractByUIDsUids, sourceUIDs...)
	w.retractByUIDsScopes = append(w.retractByUIDsScopes, scopeIDs...)
	w.retractByUIDsEvidence = evidenceSource
	return w.retractByUIDsErr
}

func observabilityCoverageMaterializationIntent() Intent {
	return Intent{
		IntentID:     "intent-covers-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainObservabilityCoverageMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestObservabilityCoverageMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingObservabilityCoverageEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}

	intent := observabilityCoverageMaterializationIntent()
	intent.Domain = DomainObservabilityCoverageCorrelation
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestObservabilityCoverageMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := ObservabilityCoverageMaterializationHandler{
		EdgeWriter:      &recordingObservabilityCoverageEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestObservabilityCoverageMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestObservabilityCoverageMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false), // PR-1 nodes phase not yet committed
	}

	_, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent())
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

func TestObservabilityCoverageMaterializationProjectsExactCoverageEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written COVERS rows = %d, want 1", len(writer.writtenRows))
	}
	if writer.writeEvidence != observabilityCoverageEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, observabilityCoverageEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" {
		t.Fatalf("write scope id = %q, want scope-1", writer.writeScopeID)
	}
	if writer.writeGenerationID != "gen-1" {
		t.Fatalf("write generation id = %q, want gen-1", writer.writeGenerationID)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestObservabilityCoverageMaterializationGapNotWritten(t *testing.T) {
	t.Parallel()

	// An RDS instance with no alarm relationship is a gap. The covered EC2 peer
	// materializes its edge; the gap target must not, and the handler must
	// succeed (graceful degrade, not failure).
	rds := awsResourceFact("fact-rds", "aws_db_instance", "db-prod",
		"arn:aws:rds:us-east-1:111122223333:db:db-prod", "db-prod", false)
	envelopes := append(ec2AlarmCoverageFixture(), rds)

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envelopes},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written COVERS rows = %d, want 1 (only the covered EC2, never the gap)", len(writer.writtenRows))
	}
	rdsUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_db_instance", "db-prod")
	for _, row := range writer.writtenRows {
		if anyToString(row["target_uid"]) == rdsUID {
			t.Fatalf("gap target %q must not be materialized as a COVERS edge", rdsUID)
		}
	}
}

func TestObservabilityCoverageMaterializationEmptyGenerationNoWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	result, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	// First generation with no prior: no retract either (the PriorGenerationCheck
	// skip), so an empty first generation is a pure no-op.
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on first empty generation", writer.retractCalls)
	}
}

func TestObservabilityCoverageMaterializationRetractsPriorGenerationEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 when a prior generation exists", writer.retractCalls)
	}
	if writer.retractEvidence != observabilityCoverageEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, observabilityCoverageEvidenceSource)
	}
}

func TestObservabilityCoverageMaterializationSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on the first generation (no prior edges to remove)", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1 (the exact coverage edge still materializes)", writer.writeCalls)
	}
}
