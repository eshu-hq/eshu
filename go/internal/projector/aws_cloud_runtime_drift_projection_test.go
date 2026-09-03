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

// This file drives the FULL enqueue path (buildProjection, not the
// awscloudruntimedrift.BuildAWSCloudRuntimeDriftReducerIntent builder in
// isolation) for the aws_cloud_runtime_drift family: it proves
// appendScopeGenerationReducerIntents still wires the extracted builder into
// dispatch after the #6057 move, and that the same aws_resource generation
// enqueues the other AWS-scope-keyed intents it always has. The builder's own
// anchor-selection, entity-key, and source-system unit coverage lives in
// internal/projector/awscloudruntimedrift/reducer_intent_test.go.

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
	// shared cloud-inventory admission (issue #2209), and cloud-image
	// materialization -- which since the #5450 retraction-safety fix triggers
	// on the SAME aws_resource fact presence DomainAWSResourceMaterialization
	// does (not on lambda_function_uses_image relationship presence), so
	// AWSCloudImageMaterializationHandler.Handle's retract-first logic still
	// runs and correctly retracts to zero in a generation with no image
	// relationship at all, like this fixture's.
	//
	// EC2 instance identity materialization (#5448) is NOT enqueued here: this is
	// a lambda scope with no ec2_instance_posture fact, and since the #5743
	// residual fix that domain triggers on the posture fact (the node it
	// augments), not on any aws_resource fact. Enqueuing it here previously left
	// its work item stuck 'pending' because its readiness gate — which waits on
	// an EC2 instance node this scope never materializes — could never open.
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
