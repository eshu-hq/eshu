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

type recordingWorkloadCloudRelationshipWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingWorkloadCloudRelationshipWriter) WriteWorkloadCloudRelationshipEdges(
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

func (w *recordingWorkloadCloudRelationshipWriter) RetractWorkloadCloudRelationshipEdges(
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

func workloadCloudRelationshipIntent() Intent {
	return Intent{
		IntentID:     "intent-workload-cloud-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainWorkloadCloudRelationshipMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func workloadCloudAWSResourceEnvelope(factID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		StableFactKey: "stable-" + factID,
		FactKind:      facts.AWSResourceFactKind,
		Payload:       payload,
		SourceRef: facts.Ref{
			SourceSystem:   "aws",
			SourceRecordID: "source-" + factID,
		},
		CollectorKind: "aws_cloud",
	}
}

func TestExtractWorkloadCloudRelationshipRowsPromotesExactWorkloadAnchor(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractWorkloadCloudRelationshipRows([]facts.Envelope{
		workloadCloudAWSResourceEnvelope("fact-aws-1", map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ssm_parameter",
			"resource_id":   "/config/orders-api/database-url",
			"environment":   "prod",
			"workload_id":   "workload:orders-api",
			"service_name":  "orders-api",
		}),
	})
	if err != nil {
		t.Fatalf("ExtractWorkloadCloudRelationshipRows() error = %v, want nil", err)
	}

	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := anyToString(rows[0]["workload_id"]), "workload:orders-api"; got != want {
		t.Fatalf("workload_id = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[0]["relationship_type"]), "USES"; got != want {
		t.Fatalf("relationship_type = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[0]["resolution_mode"]), "explicit_workload_anchor"; got != want {
		t.Fatalf("resolution_mode = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[0]["environment"]), "prod"; got != want {
		t.Fatalf("environment = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[0]["relationship_basis"]), "aws_resource_service_anchor"; got != want {
		t.Fatalf("relationship_basis = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[0]["service_anchor_source"]), "payload.workload_id+service_name"; got != want {
		t.Fatalf("service_anchor_source = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[0]["service_anchor_reason"]), "explicit_workload_and_service_anchor"; got != want {
		t.Fatalf("service_anchor_reason = %q, want %q", got, want)
	}
	if got := anyToString(rows[0]["source_fact_id"]); got == "" {
		t.Fatal("source_fact_id must be populated")
	}
	if got := anyToString(rows[0]["source_system"]); got == "" {
		t.Fatal("source_system must be populated")
	}
	if got := anyToString(rows[0]["cloud_resource_uid"]); got == "" {
		t.Fatal("cloud_resource_uid must be populated")
	}
	if got := tally.totalSkipped(); got != 0 {
		t.Fatalf("totalSkipped = %d, want 0", got)
	}
}

func TestExtractWorkloadCloudRelationshipRowsRejectsServiceOnlyAndAmbiguousAnchors(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractWorkloadCloudRelationshipRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ssm_parameter",
			"resource_id":   "/config/orders-api/database-url",
			"service_name":  "orders-api",
		}),
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ssm_parameter",
			"resource_id":   "/config/shared",
			"workload_ids":  []any{"workload:orders-api", "workload:billing-api"},
		}),
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ssm_parameter",
			"resource_id":   "/config/no-env",
			"workload_id":   "workload:orders-api",
		}),
	})
	if err != nil {
		t.Fatalf("ExtractWorkloadCloudRelationshipRows() error = %v, want nil", err)
	}

	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want no materialized edges for unsafe anchors", rows)
	}
	if got, want := tally.skipped[workloadCloudRelationshipSkipMissingWorkloadAnchor], 1; got != want {
		t.Fatalf("missing workload skips = %d, want %d", got, want)
	}
	if got, want := tally.skipped[workloadCloudRelationshipSkipAmbiguousAnchor], 1; got != want {
		t.Fatalf("ambiguous skips = %d, want %d", got, want)
	}
	if got, want := tally.skipped[workloadCloudRelationshipSkipMissingEnvironment], 1; got != want {
		t.Fatalf("missing environment skips = %d, want %d", got, want)
	}
}

func TestExtractWorkloadCloudRelationshipRowsKeepsEnvironmentSpecificEdges(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractWorkloadCloudRelationshipRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ssm_parameter",
			"resource_id":   "/config/orders-api/database-url",
			"workload_id":   "workload:orders-api",
			"environment":   "prod",
		}),
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ssm_parameter",
			"resource_id":   "/config/orders-api/database-url",
			"workload_id":   "workload:orders-api",
			"environment":   "staging",
		}),
	})
	if err != nil {
		t.Fatalf("ExtractWorkloadCloudRelationshipRows() error = %v, want nil", err)
	}

	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got := tally.totalSkipped(); got != 0 {
		t.Fatalf("totalSkipped = %d, want 0", got)
	}
	if got, want := anyToString(rows[0]["environment"]), "prod"; got != want {
		t.Fatalf("rows[0].environment = %q, want %q", got, want)
	}
	if got, want := anyToString(rows[1]["environment"]), "staging"; got != want {
		t.Fatalf("rows[1].environment = %q, want %q", got, want)
	}
}

func TestWorkloadCloudRelationshipMaterializationProjectsExactEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingWorkloadCloudRelationshipWriter{}
	handler := WorkloadCloudRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			awsResourceEnvelope(map[string]any{
				"account_id":    "111122223333",
				"region":        "us-east-1",
				"resource_type": "aws_ssm_parameter",
				"resource_id":   "/config/orders-api/database-url",
				"environment":   "prod",
				"workload_id":   "workload:orders-api",
			}),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if writer.writeEvidence != workloadCloudRelationshipEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, workloadCloudRelationshipEvidenceSource)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1", writer.retractCalls)
	}
}

func TestWorkloadCloudRelationshipMaterializationGatesOnReadiness(t *testing.T) {
	t.Parallel()

	writer := &recordingWorkloadCloudRelationshipWriter{}
	handler := WorkloadCloudRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err == nil {
		t.Fatal("expected retryable readiness error")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable, got %v", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("error must expose failure class, got %T", err)
	}
	if got, want := classified.FailureClass(), "workload_cloud_relationship_nodes_not_ready"; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before readiness: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}
