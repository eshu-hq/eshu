// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingIAMCanAssumeEdgeWriter captures CAN_ASSUME edge writes and retracts
// so tests can assert the exact materialization request.
type recordingIAMCanAssumeEdgeWriter struct {
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

func (w *recordingIAMCanAssumeEdgeWriter) WriteIAMCanAssumeEdges(
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

func (w *recordingIAMCanAssumeEdgeWriter) RetractIAMCanAssumeEdges(
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

func iamCanAssumeIntent() Intent {
	return Intent{
		IntentID:     "intent-can-assume-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainIAMCanAssumeMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// iamCanAssumeFixture is one role that trusts a role and a user (both scanned),
// plus an external service principal that must be skipped.
func iamCanAssumeFixture() []facts.Envelope {
	const acct = "123456789012"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"
	assumingRoleARN := "arn:aws:iam::123456789012:role/ci-deployer"
	assumingUserARN := "arn:aws:iam::123456789012:user/breakglass"
	return []facts.Envelope{
		iamRoleEnvelope(acct, roleARN),
		iamRoleEnvelope(acct, assumingRoleARN),
		iamUserEnvelope(acct, assumingUserARN),
		trustPermissionFact(acct, roleARN, assumingRoleARN, assumingUserARN, "ec2.amazonaws.com"),
	}
}

func TestIAMCanAssumeMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingIAMCanAssumeEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := iamCanAssumeIntent()
	intent.Domain = DomainAWSRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestIAMCanAssumeMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := IAMCanAssumeMaterializationHandler{
		EdgeWriter:      &recordingIAMCanAssumeEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), iamCanAssumeIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestIAMCanAssumeMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), iamCanAssumeIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestIAMCanAssumeMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanAssumeEdgeWriter{}
	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: iamCanAssumeFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false), // PR-1 nodes phase not yet committed
	}

	_, err := handler.Handle(context.Background(), iamCanAssumeIntent())
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

func TestIAMCanAssumeMaterializationProjectsTrustEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanAssumeEdgeWriter{}
	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanAssumeFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), iamCanAssumeIntent())
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
		t.Fatalf("written CAN_ASSUME rows = %d, want 2 (role + user; service principal skipped)", len(writer.writtenRows))
	}
	if writer.writeEvidence != iamCanAssumeEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, iamCanAssumeEvidenceSource)
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
	// Prior generation exists, so the retract runs before the write.
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1", writer.retractCalls)
	}
	if writer.retractEvidence != iamCanAssumeEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, iamCanAssumeEvidenceSource)
	}
}

func TestIAMCanAssumeMaterializationIdempotentOnReprojection(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanAssumeEdgeWriter{}
	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanAssumeFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	first, err := handler.Handle(context.Background(), iamCanAssumeIntent())
	if err != nil {
		t.Fatalf("first Handle error: %v", err)
	}
	second, err := handler.Handle(context.Background(), iamCanAssumeIntent())
	if err != nil {
		t.Fatalf("second Handle error: %v", err)
	}
	// Same row set both times: the MERGE identity is stable, so reprojection
	// converges (idempotent) rather than producing different output.
	if first.CanonicalWrites != second.CanonicalWrites {
		t.Fatalf("reprojection changed write count: first=%d second=%d", first.CanonicalWrites, second.CanonicalWrites)
	}
	if writer.writeCalls != 2 {
		t.Fatalf("writeCalls = %d, want 2 (one per reprojection)", writer.writeCalls)
	}
}

func TestIAMCanAssumeMaterializationEmptyGenerationNoWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanAssumeEdgeWriter{}
	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	result, err := handler.Handle(context.Background(), iamCanAssumeIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for empty generation", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	// No prior generation and first attempt: the retract is skipped.
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 (no prior generation, first attempt)", writer.retractCalls)
	}
}

func TestIAMCanAssumeMaterializationOnlyExternalPrincipalsNoWrite(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"
	envelopes := []facts.Envelope{
		iamRoleEnvelope(acct, roleARN),
		trustPermissionFact(acct, roleARN, "ec2.amazonaws.com", "*", "arn:aws:iam::999988887777:role/external"),
	}
	writer := &recordingIAMCanAssumeEdgeWriter{}
	handler := IAMCanAssumeMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envelopes},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), iamCanAssumeIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 (all principals external/wildcard)", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0", writer.writeCalls)
	}
	// Retract still runs (prior generation exists) to clear any stale edges from
	// a previous generation that resolved differently.
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1", writer.retractCalls)
	}
}
