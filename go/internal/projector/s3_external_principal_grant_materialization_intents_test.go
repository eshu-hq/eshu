// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func s3ExternalPrincipalGrantIntentEnvelope(
	factID,
	scopeID,
	generationID,
	principalKind,
	principalValue,
	grantOutcome string,
) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.S3ExternalPrincipalGrantFactKind,
		SchemaVersion: facts.S3ExternalPrincipalGrantSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":       "111111111111",
			"region":           "us-east-1",
			"bucket_arn":       "arn:aws:s3:::orders-artifacts",
			"bucket_name":      "orders-artifacts",
			"principal_kind":   principalKind,
			"principal_value":  principalValue,
			"grant_outcome":    grantOutcome,
			"is_cross_account": grantOutcome == "cross_account",
			"is_public":        grantOutcome == "public",
		},
	}
}

func TestBuildProjectionQueuesS3ExternalPrincipalGrantMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	envelopes := []facts.Envelope{
		s3ExternalPrincipalGrantIntentEnvelope(
			"fact-grant-1",
			scopeValue.ScopeID,
			generation.GenerationID,
			"aws_account",
			"999988887777",
			"cross_account",
		),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainS3ExternalPrincipalGrantMaterialization)
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:111111111111:us-east-1:s3"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-grant-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the first grant fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueS3ExternalPrincipalGrantWithoutGrantFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		s3PostureIntentEnvelope("fact-posture", scopeValue.ScopeID, generation.GenerationID, ""),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainS3ExternalPrincipalGrantMaterialization {
			t.Fatalf("unexpected s3 external-principal grant intent without grant facts")
		}
	}
}
