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

// recordingCloudResourceNodeWriter captures the rows handed to the node writer
// so tests can assert on the exact materialization request.
type recordingCloudResourceNodeWriter struct {
	calls          int
	rows           []map[string]any
	evidenceSource string
	err            error
}

func (w *recordingCloudResourceNodeWriter) WriteCloudResourceNodes(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.calls++
	w.rows = append(w.rows, rows...)
	w.evidenceSource = evidenceSource
	return w.err
}

func awsResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload:  payload,
	}
}

func TestAWSResourceMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := AWSResourceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingCloudResourceNodeWriter{},
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

func TestAWSResourceMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := AWSResourceMaterializationHandler{
		NodeWriter: &recordingCloudResourceNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestAWSResourceMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := AWSResourceMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestExtractCloudResourceNodeRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	if rows, err := ExtractCloudResourceNodeRows(nil); rows != nil {
		if err != nil {
			t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
		}
		t.Fatalf("rows = %v, want nil", rows)
	}
}

func TestExtractCloudResourceNodeRowsBuildsStableUID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":          "111122223333",
			"region":              "us-east-1",
			"resource_type":       "aws_iam_role",
			"resource_id":         "arn:aws:iam::111122223333:role/app",
			"arn":                 "arn:aws:iam::111122223333:role/app",
			"name":                "app",
			"state":               "active",
			"service_kind":        "iam",
			"correlation_anchors": []any{"app", "arn:aws:iam::111122223333:role/app"},
		}),
	}

	rows, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}

	wantUID := cloudResourceUID("111122223333", "us-east-1", "aws_iam_role", "arn:aws:iam::111122223333:role/app")
	if got := anyToString(rows[0]["uid"]); got != wantUID {
		t.Fatalf("uid = %q, want %q", got, wantUID)
	}
	if got := anyToString(rows[0]["arn"]); got != "arn:aws:iam::111122223333:role/app" {
		t.Fatalf("arn = %q", got)
	}
	if got := anyToString(rows[0]["resource_type"]); got != "aws_iam_role" {
		t.Fatalf("resource_type = %q", got)
	}
	anchors, ok := rows[0]["correlation_anchors"].([]string)
	if !ok {
		t.Fatalf("correlation_anchors type = %T, want []string", rows[0]["correlation_anchors"])
	}
	if len(anchors) != 2 {
		t.Fatalf("correlation_anchors = %v, want 2 entries", anchors)
	}
}

func TestExtractCloudResourceNodeRowsSkipsNonResourceFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: facts.AWSRelationshipFactKind, Payload: map[string]any{"resource_id": "ignored"}},
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-123",
		}),
	}

	rows, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (relationship facts must be skipped)", len(rows))
	}
	if got := anyToString(rows[0]["resource_id"]); got != "vpc-123" {
		t.Fatalf("resource_id = %q, want vpc-123", got)
	}
}

func TestExtractCloudResourceNodeRowsRequiresIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		// Missing resource_id and arn.
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
		}),
		// Missing resource_type.
		awsResourceEnvelope(map[string]any{
			"account_id":  "111122223333",
			"region":      "us-east-1",
			"resource_id": "vpc-123",
		}),
	}

	if rows, err := ExtractCloudResourceNodeRows(envelopes); len(rows) != 0 {
		if err != nil {
			t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
		}
		t.Fatalf("len(rows) = %d, want 0 for incomplete identity", len(rows))
	}
}

func TestExtractCloudResourceNodeRowsDeduplicatesByUID(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"account_id":    "111122223333",
		"region":        "us-east-1",
		"resource_type": "aws_ec2_vpc",
		"resource_id":   "vpc-123",
		"name":          "main",
	}
	envelopes := []facts.Envelope{
		awsResourceEnvelope(payload),
		awsResourceEnvelope(payload),
	}

	rows, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (duplicate facts must converge on one node)", len(rows))
	}
}

func TestAWSResourceMaterializationHandleWritesNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceNodeWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-123",
		}),
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_iam_role",
			"resource_id":   "arn:aws:iam::111122223333:role/app",
			"arn":           "arn:aws:iam::111122223333:role/app",
		}),
	}}

	handler := AWSResourceMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
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
	if writer.evidenceSource != awsResourceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", writer.evidenceSource, awsResourceEvidenceSource)
	}
}

func TestAWSResourceMaterializationHandleNoFactsIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceNodeWriter{}
	handler := AWSResourceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
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

func TestAWSResourceMaterializationHandlePublishesCanonicalNodesCommittedPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-123",
		}),
	}}
	handler := AWSResourceMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     &recordingCloudResourceNodeWriter{},
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (Stage B gates on this readiness phase)", len(publisher.calls))
	}
	rows := publisher.calls[0]
	if len(rows) != 1 {
		t.Fatalf("published rows = %d, want 1", len(rows))
	}
	if got, want := rows[0].Key.Keyspace, GraphProjectionKeyspaceCloudResourceUID; got != want {
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

func TestAWSResourceMaterializationHandlePublishesPhaseOnEmptyGeneration(t *testing.T) {
	t.Parallel()

	// A generation that scanned zero materializable resources must still
	// publish the canonical-nodes-committed phase, otherwise Stage B (the AWS
	// relationship edge projection) never observes that Stage A completed and
	// blocks forever on the readiness gate.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingCloudResourceNodeWriter{}
	handler := AWSResourceMaterializationHandler{
		FactLoader:     &stubFactLoader{},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (no facts must not write)", writer.calls)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (empty generation must still unblock Stage B)", len(publisher.calls))
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
}

func TestAWSResourceMaterializationHandleDoesNotPublishPhaseOnWriteFailure(t *testing.T) {
	t.Parallel()

	// Publishing the readiness gate after a failed node write would let Stage B
	// resolve edges against nodes that never committed. The phase must only be
	// published once the canonical node write succeeds.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingCloudResourceNodeWriter{err: errors.New("graph backend unavailable")}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-123",
		}),
	}}
	handler := AWSResourceMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err == nil {
		t.Fatal("expected error when node write fails")
	}

	if len(publisher.calls) != 0 {
		t.Fatalf("publisher.calls = %d, want 0 (no readiness gate after a failed write)", len(publisher.calls))
	}
}
