// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingS3InternetExposureNodeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingS3InternetExposureNodeWriter) WriteS3InternetExposureNodes(
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

func (w *recordingS3InternetExposureNodeWriter) RetractS3InternetExposureNodes(
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

func s3InternetExposureIntent() Intent {
	return Intent{
		IntentID:     "intent-s3-internet-exposure-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainS3InternetExposureMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func s3InternetExposureFixture() []facts.Envelope {
	const account = "111111111111"
	const region = "us-east-1"
	return []facts.Envelope{
		s3BucketResourceEnvelope(account, region, "orders"),
		s3InternetExposurePostureEnvelope(
			"fact-public-policy-blocked",
			account,
			region,
			"orders",
			map[string]any{
				"policy_present":          true,
				"policy_grants_public":    true,
				"restrict_public_buckets": true,
			},
		),
	}
}

func TestS3InternetExposureMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingS3InternetExposureNodeWriter{}
	handler := S3InternetExposureMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: s3InternetExposureFixture()},
		NodeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), s3InternetExposureIntent())
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

func TestS3InternetExposureMaterializationProjectsNodeProperties(t *testing.T) {
	t.Parallel()

	writer := &recordingS3InternetExposureNodeWriter{}
	handler := S3InternetExposureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: s3InternetExposureFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), s3InternetExposureIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 (prior generation exists)", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written rows = %d, want 1", len(writer.writtenRows))
	}
	row := writer.writtenRows[0]
	if got, want := row["state"], "not_exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["source_fact_id"], "fact-public-policy-blocked"; got != want {
		t.Fatalf("source_fact_id = %v, want %v", got, want)
	}
	if writer.writeEvidence != s3InternetExposureEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, s3InternetExposureEvidenceSource)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestS3InternetExposureMaterializationRetractsStalePropertiesWhenGenerationHasNoRows(t *testing.T) {
	t.Parallel()

	writer := &recordingS3InternetExposureNodeWriter{}
	handler := S3InternetExposureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), s3InternetExposureIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for empty generation", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 to remove stale prior properties", writer.retractCalls)
	}
	if got, want := writer.retractEvidence, s3InternetExposureEvidenceSource; got != want {
		t.Fatalf("retract evidence = %q, want %q", got, want)
	}
}
