// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildProjectionQueuesAWSCloudImageMaterialization is the enqueue
// regression proof for the bug a hostile review caught on #5450: the handler,
// domain constant, and retractable edge type were all wired, but nothing in
// go/internal/projector ever built a ReducerIntent for
// DomainAWSCloudImageMaterialization, so appendScopeGenerationReducerIntents
// created no work item for it and the edge never materialized in a real
// generation (only the handler+writer unit/live tests exercised it, which
// call the handler directly and bypass the enqueue path entirely). This test
// drives the FULL enqueue path (appendScopeGenerationReducerIntents, not the
// handler) and fails without buildAWSCloudImageMaterializationReducerIntent
// wired into it.
func TestBuildProjectionQueuesAWSCloudImageMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "aws:123456789012:us-east-1:lambda"}
	generation := scope.ScopeGeneration{GenerationID: "gen-lambda-1"}
	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, []facts.Envelope{
		{
			FactID:           "fact-lambda-resource-1",
			FactKind:         facts.AWSResourceFactKind,
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			SourceRef:        facts.Ref{SourceSystem: "aws"},
			SourceConfidence: facts.SourceConfidenceReported,
			Payload: map[string]any{
				"arn":           "arn:aws:lambda:us-east-1:123456789012:function:demo",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:demo",
				"resource_type": "lambda.function",
				"account_id":    "123456789012",
				"region":        "us-east-1",
				"attributes": map[string]any{
					"package_type":       "Image",
					"image_uri":          "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
					"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
				},
			},
		},
		{
			FactID:           "fact-lambda-relationship-1",
			FactKind:         facts.AWSRelationshipFactKind,
			SchemaVersion:    facts.AWSRelationshipSchemaVersion,
			SourceRef:        facts.Ref{SourceSystem: "aws"},
			SourceConfidence: facts.SourceConfidenceReported,
			Payload: map[string]any{
				"account_id":         "123456789012",
				"region":             "us-east-1",
				"relationship_type":  "lambda_function_uses_image",
				"source_resource_id": "arn:aws:lambda:us-east-1:123456789012:function:demo",
				"source_arn":         "arn:aws:lambda:us-east-1:123456789012:function:demo",
				"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
				"target_type":        "container_image",
				"attributes": map[string]any{
					"package_type":       "Image",
					"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
				},
			},
		},
	})

	intent := intentForDomain(t, intents, reducer.DomainAWSCloudImageMaterialization)
	if got, want := intent.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := intent.GenerationID, "gen-lambda-1"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	// The entity key MUST match the AWS resource materialization intent's
	// entity key so AWSCloudImageMaterializationHandler.sourceNodesReady
	// resolves the CloudResource canonical-nodes-committed phase
	// DomainAWSResourceMaterialization publishes for this exact acceptance
	// unit — a mismatch here would silently reopen the readiness-gate bug in
	// a different form (the intent enqueues, but the handler can never see
	// its source nodes as ready).
	if got, want := intent.EntityKey, "aws_resource_materialization:"+scopeValue.ScopeID; got != want {
		t.Fatalf("EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-lambda-relationship-1"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionSkipsAWSCloudImageMaterializationWithoutLambdaRelationship
// is the negative case: a generation with aws_resource/aws_relationship
// facts but no lambda_function_uses_image relationship must not enqueue
// DomainAWSCloudImageMaterialization. Covers both an unrelated
// relationship_type and the tag-only ecs_task_definition_uses_image
// relationship, which AWSCloudImageMaterializationHandler recognizes but
// always skips (stays Postgres-only per the #5472 EXACT-ONLY policy) — a
// generation with only that relationship type has no cloud-image work to
// enqueue, so it must not fire the trigger either.
func TestBuildProjectionSkipsAWSCloudImageMaterializationWithoutLambdaRelationship(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "aws:123456789012:us-east-1:ecs"}
	generation := scope.ScopeGeneration{GenerationID: "gen-ecs-1"}
	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, []facts.Envelope{
		{
			FactID:        "fact-ecs-resource-1",
			FactKind:      facts.AWSResourceFactKind,
			SchemaVersion: facts.AWSResourceSchemaVersion,
			SourceRef:     facts.Ref{SourceSystem: "aws"},
			Payload: map[string]any{
				"arn":           "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
				"resource_id":   "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
				"resource_type": "ecs.task_definition",
				"account_id":    "123456789012",
				"region":        "us-east-1",
			},
		},
		{
			FactID:        "fact-ecs-relationship-1",
			FactKind:      facts.AWSRelationshipFactKind,
			SchemaVersion: facts.AWSRelationshipSchemaVersion,
			SourceRef:     facts.Ref{SourceSystem: "aws"},
			Payload: map[string]any{
				"account_id":         "123456789012",
				"region":             "us-east-1",
				"relationship_type":  "ecs_task_definition_uses_image",
				"source_resource_id": "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
				"source_arn":         "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
				"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
				"target_type":        "container_image",
			},
		},
		{
			FactID:        "fact-unrelated-relationship-1",
			FactKind:      facts.AWSRelationshipFactKind,
			SchemaVersion: facts.AWSRelationshipSchemaVersion,
			SourceRef:     facts.Ref{SourceSystem: "aws"},
			Payload: map[string]any{
				"account_id":         "123456789012",
				"region":             "us-east-1",
				"relationship_type":  "uses_kms_key",
				"source_resource_id": "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
				"source_arn":         "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
				"target_resource_id": "arn:aws:kms:us-east-1:123456789012:key/key-1",
				"target_arn":         "arn:aws:kms:us-east-1:123456789012:key/key-1",
				"target_type":        "aws_kms_key",
			},
		},
	})

	for _, intent := range intents {
		if intent.Domain == reducer.DomainAWSCloudImageMaterialization {
			t.Fatalf("unexpected aws cloud image intent without a lambda_function_uses_image relationship: %#v", intent)
		}
	}
}
