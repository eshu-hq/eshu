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

// These cases used to live in aws_cloud_runtime_drift_intents_test.go
// alongside the aws_cloud_runtime_drift dispatch tests; they moved into their
// own file, matching aws_resource_materialization_intents.go's name, when
// that unrelated family's builder was extracted into
// internal/projector/awscloudruntimedrift (#6057).

func TestBuildProjectionQueuesAWSResourceMaterializationIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:us-east-1:lambda",
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
	envelopes := []facts.Envelope{
		awsResourceEnvelope("fact-aws-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainAWSResourceMaterialization)
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-aws-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first aws_resource fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueAWSResourceMaterializationWithoutAWSResource(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:us-east-1:lambda",
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

	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainAWSResourceMaterialization {
			t.Fatalf("unexpected aws_resource_materialization intent without aws_resource facts")
		}
	}
}
