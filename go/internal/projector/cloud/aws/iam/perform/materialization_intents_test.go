// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perform

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:123456789012:aws-global:iam"
	testGenerationID = "aws-generation-1"
)

func identityPermissionEnvelope(factID, policySource, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      facts.AWSIAMPermissionFactKind,
		SchemaVersion: facts.AWSIAMPermissionSchemaVersion,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"principal_arn": "arn:aws:iam::123456789012:role/deployable-source-app-role",
			"policy_source": policySource,
			"effect":        "Allow",
			"actions":       []any{"s3:putobject"},
			"resources":     []any{"arn:aws:s3:::scd-upload-receipts"},
		},
	}
}

func resourcePolicyPermissionEnvelope(factID, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      facts.AWSResourcePolicyPermissionFactKind,
		SchemaVersion: facts.AWSResourcePolicyPermissionSchemaVersion,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_arn":  "arn:aws:s3:::scd-upload-receipts",
			"resource_type": "aws_s3_bucket",
			"effect":        "Allow",
		},
	}
}

// TestBuildIAMCanPerformMaterializationReducerIntent proves the builder
// enqueues only when the generation carries a trustable-shaped identity
// permission statement (policy_source inline or attached_managed) or a
// resource-policy permission fact, anchors to the earliest qualifying fact in
// original input order across both kinds, keys the intent on the shared AWS
// resource materialization entity so the edge handler gates on the same
// canonical-nodes phase the IAM node builders publish, and falls back to
// CollectorKind when SourceRef's SourceSystem is blank.
func TestBuildIAMCanPerformMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from an inline identity statement, skipping a trust statement ahead of it", func(t *testing.T) {
		t.Parallel()
		// A trust statement alone must not trigger the CAN_PERFORM intent:
		// CAN_ASSUME owns trust statements. An inline identity statement is
		// what makes a CAN_PERFORM edge possible.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			identityPermissionEnvelope("fact-trust", "trust", "aws", "aws_cloud"),
			identityPermissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainIAMCanPerformMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "aws iam identity or resource-policy permission statements observed",
			FactID:    "fact-inline", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("queues from an attached_managed identity statement", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			identityPermissionEnvelope("fact-attached", "attached_managed", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-attached" {
			t.Fatalf("FactID = %q, want fact-attached", got.FactID)
		}
	})

	t.Run("queues from a resource-policy permission fact alone", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			resourcePolicyPermissionEnvelope("fact-resource-policy", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-resource-policy" {
			t.Fatalf("FactID = %q, want fact-resource-policy", got.FactID)
		}
	})

	t.Run("anchors to the earliest qualifying fact across both kinds in input order", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			identityPermissionEnvelope("fact-trust", "trust", "aws", "aws_cloud"),
			resourcePolicyPermissionEnvelope("fact-resource-policy", "aws", "aws_cloud"),
			identityPermissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-resource-policy" {
			t.Fatalf("FactID = %q, want fact-resource-policy (earliest qualifying fact in input order)", got.FactID)
		}
	})

	t.Run("does not queue from an identity statement whose payload fails decode", func(t *testing.T) {
		t.Parallel()
		invalid := identityPermissionEnvelope("fact-invalid-inline", "inline", "aws", "aws_cloud")
		delete(invalid.Payload, "principal_arn")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{invalid})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) from input_invalid permission, want zero intent and false", got, ok)
		}
	})

	t.Run("skips an undecodable identity statement and anchors the next valid one", func(t *testing.T) {
		t.Parallel()
		invalid := identityPermissionEnvelope("fact-invalid-inline", "inline", "aws", "aws_cloud")
		delete(invalid.Payload, "principal_arn")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			invalid,
			identityPermissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-inline" {
			t.Fatalf("FactID = %q, want fact-inline (first decodable qualifying statement)", got.FactID)
		}
	})

	t.Run("shares the aws_resource_materialization entity key with the node builders", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			identityPermissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
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
			identityPermissionEnvelope("fact-inline", "inline", "  ", "aws_cloud_collector"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "aws_cloud_collector")
		}
	})

	t.Run("does not queue without a qualifying fact", func(t *testing.T) {
		t.Parallel()
		// Only a trust statement: no identity or resource-policy permission
		// fact, so no CAN_PERFORM edge is possible and no intent should queue.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			identityPermissionEnvelope("fact-trust", "trust", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanPerformMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) without a qualifying fact, want zero intent and false", got, ok)
		}
	})
}
