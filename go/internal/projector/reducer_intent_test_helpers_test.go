// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// intentForDomain and awsResourceEnvelope are shared dispatcher-level test
// fixtures used by many builder-family test files in this package. They
// originally lived in aws_cloud_runtime_drift_intents_test.go; they moved
// here, unchanged, when that file's builder was extracted into
// internal/projector/awscloudruntimedrift (#6057) so every other family's
// dispatch test kept a home for them in the root package.

// intentForDomain returns the single reducer intent for domain, or fails the
// test if none is present.
func intentForDomain(t *testing.T, intents []ReducerIntent, domain reducer.Domain) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == domain {
			return intent
		}
	}
	t.Fatalf("no reducer intent found for domain %q", domain)
	return ReducerIntent{}
}

// awsResourceEnvelope returns one well-formed aws_resource fact envelope
// anchored to factID, scopeID, and generationID, suitable for driving
// buildProjection dispatch tests that need real AWS resource evidence.
func awsResourceEnvelope(factID, scopeID, generationID string) facts.Envelope {
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
			"arn":           "arn:aws:lambda:us-east-1:123456789012:function:team-api",
			"region":        "us-east-1",
			"resource_id":   "team-api",
			"resource_type": "aws_lambda_function",
			"tags": map[string]any{
				"Environment": "prod",
			},
		},
	}
}
