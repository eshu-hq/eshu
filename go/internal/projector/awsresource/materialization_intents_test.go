// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsresource

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:123456789012:us-east-1:lambda"
	testGenerationID = "aws-generation-1"
)

func resourceEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      factKind,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"arn":           "arn:aws:lambda:us-east-1:123456789012:function:team-api",
			"region":        "us-east-1",
			"resource_id":   "team-api",
			"resource_type": "aws_lambda_function",
		},
	}
}

// TestBuildAWSResourceMaterializationReducerIntent proves the builder enqueues
// from aws_resource presence alone, anchors to the earliest such fact in
// original input order, derives the aws_resource_materialization-keyed entity
// key other AWS families gate on, and derives the two-tier trimmed
// source-system label (SourceRef.SourceSystem, else CollectorKind) that
// projectorintent.SourceSystem produces.
func TestBuildAWSResourceMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from aws_resource presence, anchored to the earliest fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("fact-aws-1", facts.AWSResourceFactKind, "aws", "aws_cloud"),
			resourceEnvelope("fact-aws-2", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSResourceMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainAWSResourceMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "aws runtime resource facts observed",
			FactID:    "fact-aws-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("skips a leading non-aws_resource fact when anchoring", func(t *testing.T) {
		t.Parallel()
		// FirstOfKind selects the earliest fact OF THE REQUESTED KIND in
		// original input order, not the earliest fact overall. A substitute
		// that anchored on inputFacts[0] would pick fact-rel-1 here.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("fact-rel-1", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
			resourceEnvelope("fact-aws-2", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSResourceMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-aws-2" {
			t.Fatalf("FactID = %q, want %q (earliest aws_resource fact)", got.FactID, "fact-aws-2")
		}
	})

	t.Run("prefers a trimmed SourceRef.SourceSystem for the source label", func(t *testing.T) {
		t.Parallel()
		// Two-tier pin, first tier: a non-blank trimmed SourceRef.SourceSystem
		// wins over CollectorKind. A single-tier CollectorKind-only substitute
		// fails here.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("fact-aws-1", facts.AWSResourceFactKind, "  aws  ", "aws_cloud_collector"),
		})
		got, ok := BuildAWSResourceMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws" {
			t.Fatalf("SourceSystem = %q, want %q (trimmed SourceRef.SourceSystem wins over CollectorKind)", got.SourceSystem, "aws")
		}
	})

	t.Run("falls back to a trimmed CollectorKind when SourceSystem is blank", func(t *testing.T) {
		t.Parallel()
		// Two-tier pin, second tier: a whitespace-only SourceRef.SourceSystem
		// falls through to the trimmed CollectorKind, and a blank source ref
		// does not drop the intent.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("fact-aws-1", facts.AWSResourceFactKind, "  ", " aws_cloud_collector "),
		})
		got, ok := BuildAWSResourceMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud_collector" {
			t.Fatalf("SourceSystem = %q, want %q (trimmed CollectorKind fallback)", got.SourceSystem, "aws_cloud_collector")
		}
	})

	t.Run("does not queue without aws_resource facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("fact-rel-1", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSResourceMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}
