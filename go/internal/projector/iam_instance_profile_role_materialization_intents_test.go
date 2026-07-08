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

func iamInstanceProfileResourceFact(factID, scopeID, generationID string, roleARNs ...string) facts.Envelope {
	roles := make([]any, 0, len(roleARNs))
	for _, arn := range roleARNs {
		roles = append(roles, arn)
	}
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.AWSResourceFactKind,
		SchemaVersion: facts.AWSResourceSchemaVersion,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "aws-global",
			"resource_type": "aws_iam_instance_profile",
			"resource_id":   "arn:aws:iam::123456789012:instance-profile/app",
			"arn":           "arn:aws:iam::123456789012:instance-profile/app",
			"role_arns":     roles,
		},
	}
}

func iamInstanceProfileRoleScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:aws-global:iam",
		ScopeKind:    "aws_cloud",
		SourceSystem: "aws",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "aws-generation-1",
		ObservedAt:   time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 6, 2, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	return scopeValue, generation
}

func TestBuildProjectionQueuesIAMInstanceProfileRoleMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamInstanceProfileRoleScopeAndGeneration()
	envelopes := []facts.Envelope{
		iamInstanceProfileResourceFact("fact-profile-empty", scopeValue.ScopeID, generation.GenerationID),
		iamInstanceProfileResourceFact("fact-profile-role", scopeValue.ScopeID, generation.GenerationID,
			"arn:aws:iam::123456789012:role/app"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainIAMInstanceProfileRoleMaterialization)
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:123456789012:aws-global:iam"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-profile-empty"; got != want {
		t.Fatalf("intent.FactID = %q, want first instance-profile fact", got)
	}
}

func TestBuildProjectionQueuesIAMInstanceProfileRoleForNoRoleProfile(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamInstanceProfileRoleScopeAndGeneration()
	envelopes := []facts.Envelope{
		iamInstanceProfileResourceFact("fact-profile-empty", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainIAMInstanceProfileRoleMaterialization)
	if got, want := intent.FactID, "fact-profile-empty"; got != want {
		t.Fatalf("intent.FactID = %q, want no-role profile to trigger stale-edge retract", got)
	}
}

func TestBuildProjectionDoesNotQueueIAMInstanceProfileRoleWithoutProfile(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamInstanceProfileRoleScopeAndGeneration()
	envelopes := []facts.Envelope{{
		FactID:        "fact-role",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.AWSResourceFactKind,
		SchemaVersion: facts.AWSResourceSchemaVersion,
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "aws-global",
			"resource_type": "aws_iam_role",
			"resource_id":   "arn:aws:iam::123456789012:role/app",
			"arn":           "arn:aws:iam::123456789012:role/app",
		},
	}}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainIAMInstanceProfileRoleMaterialization {
			t.Fatalf("unexpected iam_instance_profile_role_materialization intent without profile fact")
		}
	}
}

func TestBuildProjectionDoesNotQueueIAMInstanceProfileRoleFromInvalidResource(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamInstanceProfileRoleScopeAndGeneration()
	envelopes := []facts.Envelope{
		iamInstanceProfileResourceFact("fact-invalid-profile", scopeValue.ScopeID, generation.GenerationID,
			"arn:aws:iam::123456789012:role/app"),
	}
	delete(envelopes[0].Payload, "resource_id")

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainIAMInstanceProfileRoleMaterialization {
			t.Fatalf("unexpected iam_instance_profile_role_materialization intent from input_invalid resource")
		}
	}
}
