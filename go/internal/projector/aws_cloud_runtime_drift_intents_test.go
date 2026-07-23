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

func TestBuildProjectionQueuesSingleAWSCloudRuntimeDriftIntent(t *testing.T) {
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
		awsResourceEnvelope("fact-aws-2", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	// AWS resource facts enqueue runtime-drift, CloudResource node
	// materialization (issue #805), the workload-cloud relationship slice,
	// shared cloud-inventory admission (issue #2209), and -- since the #5450
	// retraction-safety fix -- cloud-image materialization too: it now
	// triggers on the SAME aws_resource fact presence
	// DomainAWSResourceMaterialization does (not on lambda_function_uses_image
	// relationship presence), so AWSCloudImageMaterializationHandler.Handle's
	// retract-first logic still runs and correctly retracts to zero in a
	// generation with no image relationship at all, like this fixture's.
	if got, want := len(projection.reducerIntents), 5; got != want {
		t.Fatalf("len(reducerIntents) = %d, want %d", got, want)
	}
	cloudImage := intentForDomain(t, projection.reducerIntents, reducer.DomainAWSCloudImageMaterialization)
	if got, want := cloudImage.EntityKey, "aws_resource_materialization:aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("cloudImage.EntityKey = %q, want %q", got, want)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainAWSCloudRuntimeDrift)
	if got, want := intent.EntityKey, "aws_cloud_runtime_drift:aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-aws-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first aws_resource fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
	// The shared cloud-inventory admission intent is now enqueued so the
	// canonical GET /api/v0/cloud/inventory readback is populated (#2209).
	admission := intentForDomain(t, projection.reducerIntents, reducer.DomainCloudInventoryAdmission)
	if got, want := admission.EntityKey, "cloud_inventory_admission:aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("admission.EntityKey = %q, want %q", got, want)
	}
}

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

func intentForDomain(t *testing.T, intents []ReducerIntent, domain reducer.Domain) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == domain {
			return intent
		}
	}
	t.Fatalf("no reducer intent found for domain %q", domain)
	return ReducerIntent{}
}

func TestBuildProjectionDoesNotQueueAWSCloudRuntimeDriftWithoutAWSResource(t *testing.T) {
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
	if got := len(projection.reducerIntents); got != 0 {
		t.Fatalf("len(reducerIntents) = %d, want 0", got)
	}
}

func awsResourceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.AWSResourceFactKind,
		SchemaVersion:    facts.AWSResourceSchemaVersion,
		CollectorKind:    "aws_cloud",
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"arn":           "arn:aws:lambda:us-east-1:123456789012:function:team-api",
			"region":        "us-east-1",
			"resource_id":   "team-api",
			"resource_type": "aws_lambda_function",
			"tags": map[string]any{
				"Environment": "prod",
			},
		},
	}
}
