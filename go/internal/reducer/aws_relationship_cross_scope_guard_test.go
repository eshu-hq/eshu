// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Regression for #6162, AWS side. CloudResourceEdgeWriter stamps rel.scope_id
// and rel.generation_id from the EXECUTING intent, never from the row, exactly
// as the GCP writer does, so a row built against a resource scanned in another
// scope is written under this intent's scope.
//
// cloudjoin.CloudResourceJoinIndex documents the same rule the GCP index did --
// "a cross-account or cross-region ARN target resolves only if that
// account+region resource was scanned in the same scope (the trust-boundary
// rule, design §10.3)" -- and, before this change, also did not enforce it.
//
// The GCP guard landed in #6913; this mirrors it for the ARN, bare-id and
// correlation-anchor resolution paths, which all key into one uid space.
func TestExtractAWSRelationshipEdgeRowsRefusesCrossScopeEndpoint(t *testing.T) {
	t.Parallel()

	const (
		intentScope  = "aws:account:111122223333:region:us-east-1"
		foreignScope = "aws:account:999988887777:region:us-east-1"
		foreignFn    = "arn:aws:lambda:us-east-1:999988887777:function:foreign-fn"
		localKey     = "arn:aws:kms:us-east-1:111122223333:key/local-key"
	)

	resources := []facts.Envelope{
		{ScopeID: foreignScope, FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "999988887777", "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": foreignFn, "arn": foreignFn,
		}},
		{ScopeID: intentScope, FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_kms_key", "resource_id": localKey, "arn": localKey,
		}},
	}
	rels := []facts.Envelope{
		{ScopeID: intentScope, FactKind: facts.AWSRelationshipFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"relationship_type":  "USES_KMS_KEY",
			"source_resource_id": foreignFn, "source_arn": foreignFn,
			"target_resource_id": localKey, "target_arn": localKey,
			"target_type": "aws_kms_key",
		}},
	}

	rows, tally, _, err := ExtractAWSRelationshipEdgeRows(resources, rels, intentScope)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: a cross-scope source endpoint must not "+
			"produce a row, or the writer stamps it with %q", len(rows), intentScope)
	}
	if got := tally.crossScopeEndpoint["aws_kms_key"]; got != 1 {
		t.Fatalf("tally.crossScopeEndpoint[aws_kms_key] = %d, want 1: the refusal "+
			"must be counted, not silently dropped", got)
	}
	if got := tally.byRelTypeMode[relTypeMode{"USES_KMS_KEY", joinModeCrossScopeEndpoint}]; got != 1 {
		t.Fatalf("byRelTypeMode[USES_KMS_KEY/%s] = %d, want 1", joinModeCrossScopeEndpoint, got)
	}
}

// A same-scope endpoint must still resolve through the ARN path.
func TestExtractAWSRelationshipEdgeRowsAdmitsSameScopeEndpoint(t *testing.T) {
	t.Parallel()

	const (
		intentScope = "aws:account:111122223333:region:us-east-1"
		fn          = "arn:aws:lambda:us-east-1:111122223333:function:local-fn"
		key         = "arn:aws:kms:us-east-1:111122223333:key/local-key"
	)

	resources := []facts.Envelope{
		{ScopeID: intentScope, FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": fn, "arn": fn,
		}},
		{ScopeID: intentScope, FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_kms_key", "resource_id": key, "arn": key,
		}},
	}
	rels := []facts.Envelope{
		{ScopeID: intentScope, FactKind: facts.AWSRelationshipFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"relationship_type":  "USES_KMS_KEY",
			"source_resource_id": fn, "source_arn": fn,
			"target_resource_id": key, "target_arn": key,
			"target_type": "aws_kms_key",
		}},
	}

	rows, tally, _, err := ExtractAWSRelationshipEdgeRows(resources, rels, intentScope)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: a same-scope endpoint must still resolve", len(rows))
	}
	if got := len(tally.crossScopeEndpoint); got != 0 {
		t.Fatalf("len(tally.crossScopeEndpoint) = %d, want 0", got)
	}
}

// An empty intent scope keeps the pre-#6162 join so a caller that cannot supply
// a scope is not handed a guard that never fires.
func TestExtractAWSRelationshipEdgeRowsEmptyIntentScopeKeepsLegacyJoin(t *testing.T) {
	t.Parallel()

	const (
		foreignFn = "arn:aws:lambda:us-east-1:999988887777:function:foreign-fn"
		localKey  = "arn:aws:kms:us-east-1:111122223333:key/local-key"
	)

	resources := []facts.Envelope{
		{ScopeID: "scope-a", FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "999988887777", "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": foreignFn, "arn": foreignFn,
		}},
		{ScopeID: "scope-b", FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_kms_key", "resource_id": localKey, "arn": localKey,
		}},
	}
	rels := []facts.Envelope{
		{ScopeID: "scope-b", FactKind: facts.AWSRelationshipFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"relationship_type":  "USES_KMS_KEY",
			"source_resource_id": foreignFn, "source_arn": foreignFn,
			"target_resource_id": localKey, "target_arn": localKey,
			"target_type": "aws_kms_key",
		}},
	}

	rows, _, _, err := ExtractAWSRelationshipEdgeRows(resources, rels, "")
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 with an empty intent scope", len(rows))
	}
}
