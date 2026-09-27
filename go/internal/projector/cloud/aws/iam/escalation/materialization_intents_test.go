// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package escalation

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

func escalationPermissionEnvelope(factID, policySource, effect, sourceSystem, collectorKind string) facts.Envelope {
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
			"region":        "aws-global",
			"principal_arn": "arn:aws:iam::123456789012:user/attacker",
			"policy_source": policySource,
			"effect":        effect,
			"actions":       []any{"iam:createpolicyversion"},
			"resources":     []any{"arn:aws:iam::123456789012:policy/team-policy"},
		},
	}
}

// TestBuildIAMEscalationMaterializationReducerIntent proves the builder
// enqueues only when the generation carries a decodable identity
// permission statement (policy_source inline or attached_managed — a trust
// statement is the sibling trust package's trigger, not this one's),
// anchors to the earliest qualifying fact in original input order, keys the
// intent on the shared AWS resource materialization entity so the edge
// handler gates on the same canonical-nodes phase the IAM node builders
// publish, and falls back to CollectorKind when SourceRef's SourceSystem is
// blank. A Deny qualifies: it contributes to the reducer grant's deny set
// even though it arms no primitive, so a Deny-only generation still needs
// the intent fanned out. A malformed fact never fails the build: it is
// skipped as a candidate, so an invalid-only generation enqueues nothing
// while an invalid-then-valid generation still anchors the valid fact.
func TestBuildIAMEscalationMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from an inline identity statement, skipping a trust statement ahead of it", func(t *testing.T) {
		t.Parallel()
		// A trust statement alone must not trigger the escalation intent:
		// trust statements are the sibling trust package's trigger.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			escalationPermissionEnvelope("fact-trust", "trust", "Allow", "aws", "aws"),
			escalationPermissionEnvelope("fact-inline", "inline", "Allow", "aws", "aws"),
		})
		intent, ok := BuildIAMEscalationMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("expected an intent from the inline identity statement")
		}
		if intent.FactID != "fact-inline" {
			t.Fatalf("intent anchored to %q, want the earliest qualifying fact %q", intent.FactID, "fact-inline")
		}
		if intent.Domain != reducer.DomainIAMEscalationMaterialization {
			t.Fatalf("intent domain = %q, want %q", intent.Domain, reducer.DomainIAMEscalationMaterialization)
		}
		if want := "aws_resource_materialization:" + testScopeID; intent.EntityKey != want {
			t.Fatalf("intent entity key = %q, want %q", intent.EntityKey, want)
		}
	})

	t.Run("a Deny qualifies", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			escalationPermissionEnvelope("fact-deny", "inline", "Deny", "aws", "aws"),
		})
		if _, ok := BuildIAMEscalationMaterializationReducerIntent(testScopeID, testGenerationID, lookup); !ok {
			t.Fatal("expected an intent from a Deny identity statement")
		}
	})

	t.Run("no intent without a qualifying statement", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			escalationPermissionEnvelope("fact-trust-only", "trust", "Allow", "aws", "aws"),
		})
		if _, ok := BuildIAMEscalationMaterializationReducerIntent(testScopeID, testGenerationID, lookup); ok {
			t.Fatal("expected no intent from a trust-only generation")
		}
	})

	t.Run("does not queue from a statement whose payload fails decode", func(t *testing.T) {
		t.Parallel()
		invalid := escalationPermissionEnvelope("fact-invalid-inline", "inline", "Allow", "aws", "aws")
		delete(invalid.Payload, "principal_arn")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{invalid})
		got, ok := BuildIAMEscalationMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) from input_invalid permission, want zero intent and false", got, ok)
		}
	})

	t.Run("skips an undecodable statement and anchors the next valid one", func(t *testing.T) {
		t.Parallel()
		invalid := escalationPermissionEnvelope("fact-invalid-inline", "inline", "Allow", "aws", "aws")
		delete(invalid.Payload, "principal_arn")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			invalid,
			escalationPermissionEnvelope("fact-inline", "inline", "Allow", "aws", "aws"),
		})
		got, ok := BuildIAMEscalationMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-inline" {
			t.Fatalf("FactID = %q, want fact-inline (first decodable qualifying statement)", got.FactID)
		}
	})
}
