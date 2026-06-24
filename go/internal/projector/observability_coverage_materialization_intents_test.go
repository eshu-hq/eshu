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

func observabilityAWSResourceEnvelope(factID, scopeID, generationID, resourceType string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.AWSResourceFactKind,
		SchemaVersion: facts.AWSResourceSchemaVersion,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"arn":           "arn:aws:cloudwatch:us-east-1:123456789012:alarm:cpu-high",
			"resource_id":   "cpu-high",
			"resource_type": resourceType,
		},
	}
}

func observabilityCoverageScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
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
	return scopeValue, generation
}

func TestBuildProjectionQueuesObservabilityCoverageMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	envelopes := []facts.Envelope{
		// A non-observability resource alone must not trigger the coverage edge
		// intent; the alarm is what makes a COVERS edge possible.
		awsResourceEnvelope("fact-lambda", scopeValue.ScopeID, generation.GenerationID),
		observabilityAWSResourceEnvelope("fact-alarm", scopeValue.ScopeID, generation.GenerationID, "aws_cloudwatch_alarm"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainObservabilityCoverageMaterialization)
	// The entity key must match the AWS resource materialization intent so the
	// handler's readiness gate resolves the same canonical-nodes slice.
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-alarm"; got != want {
		t.Fatalf("intent.FactID = %q, want the observability fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueObservabilityCoverageWithoutObservabilityResource(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	// Only a plain Lambda aws_resource fact: no observability object, so no
	// COVERS edge is possible and no materialization intent should be queued.
	envelopes := []facts.Envelope{
		awsResourceEnvelope("fact-lambda", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainObservabilityCoverageMaterialization {
			t.Fatalf("unexpected observability_coverage_materialization intent without an observability resource")
		}
	}
}
