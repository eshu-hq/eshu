// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadcloud

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:111111111111:us-east-1:workload"
	testGenerationID = "aws-generation-1"
)

func awsResourceEnvelope(factID, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      facts.AWSResourceFactKind,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"account_id": "111111111111",
			"region":     "us-east-1",
		},
	}
}

// TestBuildWorkloadCloudRelationshipMaterializationReducerIntent proves the
// builder enqueues from aws_resource presence alone, anchors to the earliest
// fact, shares the aws_resource_materialization entity key with the
// CloudResource node phase, and falls back to CollectorKind when SourceRef's
// SourceSystem is blank.
func TestBuildWorkloadCloudRelationshipMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from aws_resource presence, anchored to the earliest fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			awsResourceEnvelope("fact-resource-1", "aws", "aws_cloud"),
			awsResourceEnvelope("fact-resource-2", "aws", "aws_cloud"),
		})
		got, ok := BuildWorkloadCloudRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainWorkloadCloudRelationshipMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "aws resource workload anchors observed",
			FactID:    "fact-resource-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("falls back to CollectorKind when SourceSystem is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			awsResourceEnvelope("fact-resource-1", "", "aws_cloud"),
		})
		got, ok := BuildWorkloadCloudRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "aws_cloud")
		}
	})

	t.Run("does not queue without aws_resource facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup(nil)
		got, ok := BuildWorkloadCloudRelationshipMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}
