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

func ec2UsesProfileIntentEnvelope(factID, scopeID, generationID, instanceID, profileARN string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.EC2InstancePostureFactKind,
		SchemaVersion: facts.EC2InstancePostureSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":           "111122223333",
			"region":               "us-east-1",
			"instance_id":          instanceID,
			"instance_profile_arn": profileARN,
		},
	}
}

func TestBuildProjectionDoesNotQueueEC2UsesProfileFromInvalidPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	envelopes := []facts.Envelope{
		ec2UsesProfileIntentEnvelope("fact-invalid-profile", scopeValue.ScopeID, generation.GenerationID, "i-invalid",
			"arn:aws:iam::111122223333:instance-profile/app"),
	}
	delete(envelopes[0].Payload, "account_id")

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2UsesProfileMaterialization {
			t.Fatalf("unexpected ec2_uses_profile_materialization intent from input_invalid posture")
		}
	}
}

func ec2UsesProfileScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:111122223333:us-east-1:ec2",
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

func TestBuildProjectionQueuesEC2UsesProfileMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	envelopes := []facts.Envelope{
		// An instance with no profile alone must not trigger the edge intent.
		ec2UsesProfileIntentEnvelope("fact-noprofile", scopeValue.ScopeID, generation.GenerationID, "i-noprofile", ""),
		ec2UsesProfileIntentEnvelope("fact-profile", scopeValue.ScopeID, generation.GenerationID, "i-aaa",
			"arn:aws:iam::111122223333:instance-profile/app"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainEC2UsesProfileMaterialization)
	// The edge intent carries its OWN distinct entity key — NOT either node phase's
	// key — because it gates on TWO node phases under different keys via the durable
	// claim gate, not on a single matching entity key.
	if got, want := intent.EntityKey, "ec2_uses_profile_materialization:aws:111122223333:us-east-1:ec2"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-profile"; got != want {
		t.Fatalf("intent.FactID = %q, want the profile-bearing posture fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueEC2UsesProfileWithoutProfile(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	// Only an instance with no attached profile: no USES_PROFILE edge is possible
	// and no materialization intent should queue.
	envelopes := []facts.Envelope{
		ec2UsesProfileIntentEnvelope("fact-noprofile", scopeValue.ScopeID, generation.GenerationID, "i-noprofile", ""),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2UsesProfileMaterialization {
			t.Fatalf("unexpected ec2_uses_profile_materialization intent without an attached profile")
		}
	}
}
