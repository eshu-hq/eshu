// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func s3PostureIntentEnvelope(factID, scopeID, generationID, loggingTarget string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.S3BucketPostureFactKind,
		SchemaVersion: facts.S3BucketPostureSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":            "111111111111",
			"region":                "us-east-1",
			"bucket_arn":            "arn:aws:s3:::orders",
			"bucket_name":           "orders",
			"logging_target_bucket": loggingTarget,
		},
	}
}

func TestBuildProjectionDoesNotQueueS3LogsToFromInvalidPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	envelopes := []facts.Envelope{
		s3PostureIntentEnvelope("fact-invalid-logging", scopeValue.ScopeID, generation.GenerationID, "central-logs"),
	}
	delete(envelopes[0].Payload, "account_id")

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainS3LogsToMaterialization {
			t.Fatalf("unexpected s3_logs_to_materialization intent from input_invalid posture")
		}
	}
}

func s3LogsToScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:111111111111:us-east-1:s3",
		ScopeKind:    "aws_cloud",
		SourceSystem: "aws",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "aws-generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	return scopeValue, generation
}

func TestBuildProjectionQueuesS3LogsToMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	envelopes := []facts.Envelope{
		// A logging-disabled posture fact alone must not trigger the edge intent.
		s3PostureIntentEnvelope("fact-disabled", scopeValue.ScopeID, generation.GenerationID, ""),
		s3PostureIntentEnvelope("fact-logging", scopeValue.ScopeID, generation.GenerationID, "central-logs"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainS3LogsToMaterialization)
	// The entity key must match the AWS resource materialization intent so the
	// handler's readiness gate resolves the same canonical-nodes slice.
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:111111111111:us-east-1:s3"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-logging"; got != want {
		t.Fatalf("intent.FactID = %q, want the logging-enabled posture fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueS3LogsToWithoutLoggingTarget(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	// Only a logging-disabled posture fact: no log target, so no LOGS_TO edge is
	// possible and no materialization intent should queue.
	envelopes := []facts.Envelope{
		s3PostureIntentEnvelope("fact-disabled", scopeValue.ScopeID, generation.GenerationID, ""),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainS3LogsToMaterialization {
			t.Fatalf("unexpected s3_logs_to_materialization intent without a logging target")
		}
	}
}
