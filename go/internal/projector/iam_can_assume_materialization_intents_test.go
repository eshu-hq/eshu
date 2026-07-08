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

func iamTrustPermissionEnvelope(factID, scopeID, generationID, policySource string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.AWSIAMPermissionFactKind,
		SchemaVersion: facts.AWSIAMPermissionSchemaVersion,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":        "123456789012",
			"region":            "aws-global",
			"principal_arn":     "arn:aws:iam::123456789012:role/eshu-runtime",
			"policy_source":     policySource,
			"effect":            "Allow",
			"assume_principals": []any{"arn:aws:iam::123456789012:role/ci-deployer"},
		},
	}
}

func TestBuildProjectionDoesNotQueueIAMCanAssumeFromInvalidPermission(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamCanAssumeScopeAndGeneration()
	envelopes := []facts.Envelope{
		iamTrustPermissionEnvelope("fact-invalid-trust", scopeValue.ScopeID, generation.GenerationID, "trust"),
	}
	delete(envelopes[0].Payload, "principal_arn")

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainIAMCanAssumeMaterialization {
			t.Fatalf("unexpected iam_can_assume_materialization intent from input_invalid permission")
		}
	}
}

func iamCanAssumeScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:aws-global:iam",
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

func TestBuildProjectionQueuesIAMCanAssumeMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamCanAssumeScopeAndGeneration()
	envelopes := []facts.Envelope{
		// An inline identity statement alone must not trigger the trust edge
		// intent; the trust statement is what makes a CAN_ASSUME edge possible.
		iamTrustPermissionEnvelope("fact-inline", scopeValue.ScopeID, generation.GenerationID, "inline"),
		iamTrustPermissionEnvelope("fact-trust", scopeValue.ScopeID, generation.GenerationID, "trust"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainIAMCanAssumeMaterialization)
	// The entity key must match the AWS resource materialization intent so the
	// handler's readiness gate resolves the same canonical-nodes slice.
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:123456789012:aws-global:iam"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-trust"; got != want {
		t.Fatalf("intent.FactID = %q, want the trust statement fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueIAMCanAssumeWithoutTrustStatement(t *testing.T) {
	t.Parallel()

	scopeValue, generation := iamCanAssumeScopeAndGeneration()
	// Only an inline identity-policy permission fact: no trust statement, so no
	// CAN_ASSUME edge is possible and no materialization intent should queue.
	envelopes := []facts.Envelope{
		iamTrustPermissionEnvelope("fact-inline", scopeValue.ScopeID, generation.GenerationID, "inline"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainIAMCanAssumeMaterialization {
			t.Fatalf("unexpected iam_can_assume_materialization intent without a trust statement")
		}
	}
}
