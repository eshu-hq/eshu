// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcanassume

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

func permissionEnvelope(factID, policySource, sourceSystem, collectorKind string) facts.Envelope {
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
			"account_id":        "123456789012",
			"region":            "aws-global",
			"principal_arn":     "arn:aws:iam::123456789012:role/eshu-runtime",
			"policy_source":     policySource,
			"effect":            "Allow",
			"assume_principals": []any{"arn:aws:iam::123456789012:role/ci-deployer"},
		},
	}
}

// TestBuildIAMCanAssumeMaterializationReducerIntent proves the builder enqueues
// only when a trust-source aws_iam_permission fact exists, anchors to the
// earliest such fact in original input order while skipping identity-policy
// statements and facts whose payload fails the typed decode, keys the intent on
// the shared AWS resource materialization entity so the edge handler gates on
// the same canonical-nodes phase the IAM node builders publish, and falls back
// to CollectorKind when SourceRef's SourceSystem is blank.
func TestBuildIAMCanAssumeMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from a trust statement, skipping the inline statement ahead of it", func(t *testing.T) {
		t.Parallel()
		// An inline identity statement alone must not trigger the trust edge
		// intent; the trust statement is what makes a CAN_ASSUME edge possible.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			permissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
			permissionEnvelope("fact-trust", "trust", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainIAMCanAssumeMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "aws iam trust statements observed",
			FactID:    "fact-trust", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("anchors to the earliest trust statement in input order", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			permissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
			permissionEnvelope("fact-trust-1", "trust", "aws", "aws_cloud"),
			permissionEnvelope("fact-trust-2", "trust", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-trust-1" {
			t.Fatalf("FactID = %q, want fact-trust-1 (earliest trust statement in input order)", got.FactID)
		}
	})

	t.Run("does not queue from a trust statement whose payload fails decode", func(t *testing.T) {
		t.Parallel()
		invalid := permissionEnvelope("fact-invalid-trust", "trust", "aws", "aws_cloud")
		delete(invalid.Payload, "principal_arn")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{invalid})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) from input_invalid permission, want zero intent and false", got, ok)
		}
	})

	t.Run("skips an undecodable trust statement and anchors the next valid one", func(t *testing.T) {
		t.Parallel()
		// The decode predicate must reject the malformed candidate and keep
		// scanning rather than abort the build: a valid trust statement later
		// in the same generation still anchors the intent.
		invalid := permissionEnvelope("fact-invalid-trust", "trust", "aws", "aws_cloud")
		delete(invalid.Payload, "principal_arn")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			invalid,
			permissionEnvelope("fact-trust", "trust", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-trust" {
			t.Fatalf("FactID = %q, want fact-trust (first decodable trust statement)", got.FactID)
		}
	})

	t.Run("shares the aws_resource_materialization entity key with the node builders", func(t *testing.T) {
		t.Parallel()
		// The edge handler's readiness gate resolves the canonical-nodes
		// committed row the AWS resource builders publish under this exact
		// key; a family-distinct key would let trust edges project before the
		// IAM role/user nodes commit.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			permissionEnvelope("fact-trust", "trust", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
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
			permissionEnvelope("fact-trust", "trust", "  ", "aws_cloud_collector"),
		})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "aws_cloud_collector")
		}
	})

	t.Run("does not queue without a trust statement", func(t *testing.T) {
		t.Parallel()
		// Only an inline identity-policy permission fact: no trust statement,
		// so no CAN_ASSUME edge is possible and no intent should queue.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			permissionEnvelope("fact-inline", "inline", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMCanAssumeMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) without a trust statement, want zero intent and false", got, ok)
		}
	})
}
