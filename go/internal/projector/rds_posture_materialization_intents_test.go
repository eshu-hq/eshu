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

func TestBuildProjectionQueuesRDSPostureMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := rdsPostureScopeAndGeneration()
	envelopes := []facts.Envelope{
		rdsPostureIntentEnvelope("fact-rds-posture-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainRDSPostureMaterialization)
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:111111111111:us-east-1:rds"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-rds-posture-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first rds_instance_posture fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueRDSPostureWithoutPostureFact(t *testing.T) {
	t.Parallel()

	scopeValue, generation := rdsPostureScopeAndGeneration()
	envelopes := []facts.Envelope{
		awsResourceEnvelope("fact-aws-rds", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainRDSPostureMaterialization {
			t.Fatalf("unexpected rds_posture_materialization intent without rds_instance_posture facts")
		}
	}
}

func rdsPostureScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:111111111111:us-east-1:rds",
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

func rdsPostureIntentEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.RDSInstancePostureFactKind,
		SchemaVersion: facts.RDSPostureSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"resource_id":         "orders-db",
			"resource_type":       "aws_rds_db_instance",
			"publicly_accessible": false,
		},
	}
}
