// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsrelationship

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:account:123456789012"
	testGenerationID = "aws:generation-1"
)

func relationshipEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      factKind,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"provider":          "aws",
			"source_arn":        "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc",
			"target_arn":        "arn:aws:ec2:us-east-1:123456789012:security-group/sg-0abc",
			"relationship_type": "member_of",
		},
	}
}

// TestBuildAWSRelationshipMaterializationReducerIntent proves the builder
// enqueues from an aws_relationship fact, anchors to the earliest such fact in
// original input order, keys the intent on the shared AWS resource
// materialization entity so the edge handler gates on the same
// canonical-nodes phase the node builders publish, and falls back to
// CollectorKind when SourceRef's SourceSystem is blank.
func TestBuildAWSRelationshipMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from aws_relationship, anchored to the earliest fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			relationshipEnvelope("relationship-fact-1", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
			relationshipEnvelope("relationship-fact-2", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainAWSRelationshipMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "aws runtime relationship facts observed",
			FactID:    "relationship-fact-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("anchors to the earliest aws_relationship fact, skipping other kinds", func(t *testing.T) {
		t.Parallel()
		// An aws_resource fact precedes the relationship facts in input order;
		// the builder must ignore it and anchor to the first aws_relationship
		// fact, not the first fact of the generation.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			relationshipEnvelope("resource-fact-1", facts.AWSResourceFactKind, "aws", "aws_cloud"),
			relationshipEnvelope("relationship-fact-2", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
			relationshipEnvelope("relationship-fact-3", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "relationship-fact-2" {
			t.Fatalf("FactID = %q, want relationship-fact-2 (earliest aws_relationship in input order)", got.FactID)
		}
	})

	t.Run("shares the aws_resource_materialization entity key with the node builders", func(t *testing.T) {
		t.Parallel()
		// The edge handler's readiness gate resolves the canonical-nodes
		// committed row the AWS resource builders publish under this exact
		// key; a family-distinct key would let edges project before nodes.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			relationshipEnvelope("relationship-fact-1", facts.AWSRelationshipFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if want := "aws_resource_materialization:" + testScopeID; got.EntityKey != want {
			t.Fatalf("EntityKey = %q, want %q", got.EntityKey, want)
		}
	})

	t.Run("falls back to CollectorKind when SourceSystem is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			relationshipEnvelope("relationship-fact-1", facts.AWSRelationshipFactKind, "  ", "aws_cloud_collector"),
		})
		got, ok := BuildAWSRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "aws_cloud_collector")
		}
	})

	t.Run("does not queue without aws_relationship facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			relationshipEnvelope("resource-fact-1", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildAWSRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}
