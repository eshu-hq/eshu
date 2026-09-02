// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloudimage

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
	testGenerationID = "gen-lambda-1"
)

func resourceEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      factKind,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"arn":           "arn:aws:lambda:us-east-1:123456789012:function:demo",
			"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:demo",
			"resource_type": "lambda.function",
			"account_id":    "123456789012",
			"region":        "us-east-1",
		},
	}
}

// TestBuildAWSCloudImageMaterializationReducerIntent proves the builder
// enqueues from aws_resource presence alone (the #5450 retraction-safety
// trigger — NOT lambda_function_uses_image relationship presence), anchors to
// the earliest aws_resource fact in original input order, keys the intent on
// the shared aws_resource_materialization entity so
// AWSCloudImageMaterializationHandler.sourceNodesReady gates on the exact
// canonical-nodes phase the AWS node builders publish, and derives the
// two-tier source-system label (SourceRef.SourceSystem trimmed, falling back
// to a trimmed CollectorKind) the root awsCloudRuntimeDriftSourceSystem
// helper produced before the extraction.
func TestBuildAWSCloudImageMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from aws_resource presence, anchored to the earliest fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("resource-fact-1", facts.AWSResourceFactKind, "aws", "aws_cloud"),
			resourceEnvelope("resource-fact-2", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSCloudImageMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainAWSCloudImageMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "aws runtime resource facts observed",
			FactID:    "resource-fact-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("triggers without any lambda_function_uses_image relationship fact", func(t *testing.T) {
		t.Parallel()
		// Retraction safety (#5450 follow-up): a generation where a Lambda's
		// image relationship disappeared must still enqueue so the handler's
		// retract-first logic runs. aws_resource presence ALONE is the trigger;
		// an aws_relationship fact is neither required nor the anchor.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("relationship-fact-1", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
			resourceEnvelope("resource-fact-2", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSCloudImageMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "resource-fact-2" {
			t.Fatalf("FactID = %q, want resource-fact-2 (anchored to the earliest aws_resource fact, not a relationship fact)", got.FactID)
		}
	})

	t.Run("shares the aws_resource_materialization entity key with the node builders", func(t *testing.T) {
		t.Parallel()
		// AWSCloudImageMaterializationHandler.sourceNodesReady resolves the
		// canonical-nodes-committed row DomainAWSResourceMaterialization
		// publishes under this exact key; a family-distinct key would silently
		// reopen the readiness-gate bug (the intent enqueues, but the handler
		// can never see its source nodes as ready).
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("resource-fact-1", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSCloudImageMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if want := "aws_resource_materialization:" + testScopeID; got.EntityKey != want {
			t.Fatalf("EntityKey = %q, want %q", got.EntityKey, want)
		}
	})

	t.Run("prefers a trimmed SourceRef.SourceSystem for the source label", func(t *testing.T) {
		t.Parallel()
		// Two-tier pin, first tier: the pre-extraction root helper
		// (awsCloudRuntimeDriftSourceSystem) preferred a non-blank trimmed
		// SourceRef.SourceSystem over CollectorKind. A single-tier
		// CollectorKind-only substitute fails here.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("resource-fact-1", facts.AWSResourceFactKind, "  aws  ", "aws_cloud_collector"),
		})
		got, ok := BuildAWSCloudImageMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
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
			resourceEnvelope("resource-fact-1", facts.AWSResourceFactKind, "  ", " aws_cloud_collector "),
		})
		got, ok := BuildAWSCloudImageMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud_collector" {
			t.Fatalf("SourceSystem = %q, want %q (trimmed CollectorKind fallback)", got.SourceSystem, "aws_cloud_collector")
		}
	})

	t.Run("does not queue without aws_resource facts", func(t *testing.T) {
		t.Parallel()
		// True negative (#5450): a generation the AWS collector did not
		// observe has nothing for sourceNodesReady to gate on and no
		// aws_resource_materialization intent would exist either.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourceEnvelope("relationship-fact-1", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSCloudImageMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}
