// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestAssertFactKinds(t *testing.T) {
	contract := Contract{
		CollectorKind: "aws",
		FactKinds: []FactKindShape{
			{Kind: facts.AWSResourceFactKind},
			{Kind: facts.AWSRelationshipFactKind},
		},
	}
	envelopes := []facts.Envelope{
		{FactKind: facts.AWSResourceFactKind},
		{FactKind: facts.AWSRelationshipFactKind},
	}
	AssertFactShape(t, contract, envelopes) // should not fail
}

func TestAssertRequiredPayloadKeys(t *testing.T) {
	contract := Contract{
		CollectorKind: "aws",
		FactKinds: []FactKindShape{
			{Kind: facts.AWSResourceFactKind, RequiredPayloadKeys: []string{"arn", "resource_type"}},
		},
	}
	envelopes := []facts.Envelope{
		{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{"arn": "a", "resource_type": "t"}},
	}
	AssertFactShape(t, contract, envelopes)
}

func TestValidateCollectorKind(t *testing.T) {
	contract := Contract{
		CollectorKind: "aws",
		FactKinds: []FactKindShape{
			{Kind: facts.AWSResourceFactKind},
		},
	}
	envelopes := []facts.Envelope{
		{FactKind: facts.AWSResourceFactKind, CollectorKind: "aws"},
	}
	ValidateCollectorKind(t, contract, envelopes) // should not fail
}

func TestEnvelopeCounts(t *testing.T) {
	envelopes := []facts.Envelope{
		{FactKind: facts.AWSResourceFactKind},
		{FactKind: facts.AWSResourceFactKind},
		{FactKind: facts.AWSRelationshipFactKind},
	}
	counts := EnvelopeCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 2 {
		t.Errorf("aws_resource count = %d, want 2", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 1 {
		t.Errorf("aws_relationship count = %d, want 1", counts[facts.AWSRelationshipFactKind])
	}
}

func TestAssertRejectsMismatchedServiceKind(t *testing.T) {
	scan := func(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
		return nil, errors.New("s3 scanner received service_kind \"sns\"")
	}
	boundary := awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         "s3",
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:s3:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
	}
	AssertRejectsMismatchedServiceKind(t, scan, boundary, "sns") // should not fail
}

func TestAssertRequiresClient(t *testing.T) {
	scan := func(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
		return nil, errors.New("s3 scanner client is required")
	}
	boundary := awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         "s3",
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:s3:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
	}
	AssertRequiresClient(t, scan, boundary) // should not fail
}
