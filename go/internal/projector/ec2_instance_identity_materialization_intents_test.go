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

// TestBuildProjectionQueuesEC2InstanceIdentityMaterialization is the #5448
// enqueue-path proof: it drives the real appendScopeGenerationReducerIntents
// fan-out (through buildProjection) end to end, proving the domain gets an
// Intent when its trigger fact is present — not just that the handler and
// writer behave correctly in isolation (which
// ec2_instance_identity_materialization_test.go and
// go/internal/storage/cypher/ec2_instance_identity_node_writer_test.go already
// cover). Without this wiring the domain would be registered but never run.
func TestBuildProjectionQueuesEC2InstanceIdentityMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2InstanceIdentityScopeAndGeneration()
	envelopes := []facts.Envelope{
		ec2InstanceIdentityAWSResourceEnvelope("fact-ec2-identity-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainEC2InstanceIdentityMaterialization)
	if got, want := intent.EntityKey, "ec2_instance_node_materialization:aws:123456789012:us-east-1:ec2"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q (must match the EC2 instance node phase, not the generic aws_resource phase)", got, want)
	}
	if got, want := intent.FactID, "fact-ec2-identity-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first aws_resource fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutAWSResource is the
// fail-before half of the enqueue-path proof: with no aws_resource fact in the
// generation, no ec2_instance_identity_materialization intent is emitted.
func TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutAWSResource(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2InstanceIdentityScopeAndGeneration()

	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2InstanceIdentityMaterialization {
			t.Fatalf("unexpected ec2_instance_identity_materialization intent without aws_resource facts")
		}
	}
}

func ec2InstanceIdentityScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:us-east-1:ec2",
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

func ec2InstanceIdentityAWSResourceEnvelope(factID, scopeID, generationID string) facts.Envelope {
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
			"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0000000000000000a",
			"region":        "us-east-1",
			"resource_id":   "i-0000000000000000a",
			"resource_type": "aws_ec2_instance",
			"attributes": map[string]any{
				"ami_id": "ami-0000000000000000a",
			},
		},
	}
}
