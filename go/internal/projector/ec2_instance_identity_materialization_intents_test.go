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
//
// The trigger is the ec2_instance_posture fact (#5743 residual fix): the domain
// augments the EC2 instance node the POSTURE path materializes and is
// readiness-gated on that node committing, so it must enqueue exactly when the
// node does. The ami_id it writes still comes from the co-present
// aws_ec2_instance aws_resource fact, which the handler loads itself.
func TestBuildProjectionQueuesEC2InstanceIdentityMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2InstanceIdentityScopeAndGeneration()
	envelopes := []facts.Envelope{
		ec2InstanceIdentityPostureEnvelope("fact-ec2-posture-1", scopeValue.ScopeID, generation.GenerationID),
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
	if got, want := intent.FactID, "fact-ec2-posture-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the first ec2_instance_posture fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutPosture is the
// #5743 residual regression: an aws scope carrying aws_resource facts but NO
// ec2_instance_posture fact (e.g. an ecr/lambda/ecs scope with no EC2 instance)
// must NOT get an ec2_instance_identity_materialization intent. Before this fix
// the builder triggered on any aws_resource fact, so such an intent was enqueued
// but its readiness gate — which waits on the EC2 instance node that a
// no-EC2-instance scope never materializes — never opened, leaving the work item
// stuck 'pending' forever (the golden-corpus fact_work_items_residual=3 failure).
func TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2InstanceIdentityScopeAndGeneration()
	envelopes := []facts.Envelope{
		ec2InstanceIdentityAWSResourceEnvelope("fact-non-ec2-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2InstanceIdentityMaterialization {
			t.Fatalf("unexpected ec2_instance_identity_materialization intent for a scope with no ec2_instance_posture fact")
		}
	}
}

// TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutFacts is the
// empty-generation half of the enqueue-path proof.
func TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2InstanceIdentityScopeAndGeneration()

	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2InstanceIdentityMaterialization {
			t.Fatalf("unexpected ec2_instance_identity_materialization intent without facts")
		}
	}
}

func ec2InstanceIdentityPostureEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.EC2InstancePostureFactKind,
		SchemaVersion:    facts.EC2InstancePostureSchemaVersionV1,
		CollectorKind:    "aws_cloud",
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":  "123456789012",
			"region":      "us-east-1",
			"resource_id": "i-0000000000000000a",
		},
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
