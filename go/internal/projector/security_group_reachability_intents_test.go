// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func sgReachabilityScope() scope.IngestionScope {
	return scope.IngestionScope{ScopeID: "aws:111122223333:us-east-1"}
}

func sgReachabilityGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{GenerationID: "gen-1"}
}

func sgRuleFactEnvelope() facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSSecurityGroupRuleFactKind,
		FactID:   "fact-sg-rule-1",
		Payload: map[string]any{
			"account_id":  "111122223333",
			"region":      "us-east-1",
			"group_id":    "sg-0abc",
			"direction":   "ingress",
			"ip_protocol": "tcp",
			"source_kind": "cidr_ipv4",
		},
	}
}

// TestSecurityGroupReachabilityIntentsFireOnRuleFacts proves all three
// reachability domains (endpoint nodes, rule nodes, edges) enqueue one intent
// each when an aws_security_group_rule fact is present, all keyed to the shared
// aws_resource_materialization acceptance unit so their readiness rows align.
func TestSecurityGroupReachabilityIntentsFireOnRuleFacts(t *testing.T) {
	t.Parallel()

	scopeValue := sgReachabilityScope()
	generation := sgReachabilityGeneration()
	index := newReducerIntentFactIndex([]facts.Envelope{sgRuleFactEnvelope()})

	cases := []struct {
		name    string
		build   func(scope.IngestionScope, scope.ScopeGeneration, *reducerIntentFactIndex) (ReducerIntent, bool)
		wantDom reducer.Domain
	}{
		{"endpoint", buildSecurityGroupEndpointMaterializationReducerIntent, reducer.DomainSecurityGroupCidrMaterialization},
		{"rule_node", buildSecurityGroupRuleMaterializationReducerIntent, reducer.DomainSecurityGroupRuleMaterialization},
		{"edge", buildSecurityGroupReachabilityMaterializationReducerIntent, reducer.DomainSecurityGroupReachabilityMaterialization},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			intent, ok := tc.build(scopeValue, generation, index)
			if !ok {
				t.Fatalf("%s intent should fire when a rule fact is present", tc.name)
			}
			if intent.Domain != tc.wantDom {
				t.Fatalf("domain = %q, want %q", intent.Domain, tc.wantDom)
			}
			if intent.EntityKey != "aws_resource_materialization:aws:111122223333:us-east-1" {
				t.Fatalf("entity key = %q, want the shared aws_resource_materialization acceptance unit", intent.EntityKey)
			}
			if intent.FactID != "fact-sg-rule-1" {
				t.Fatalf("intent must anchor to the first rule fact, got %q", intent.FactID)
			}
		})
	}
}

// TestSecurityGroupReachabilityIntentsSkipWithoutRuleFacts proves none of the
// three domains enqueue an intent when no aws_security_group_rule fact is present
// (no rule => no reachability node or edge to materialize).
func TestSecurityGroupReachabilityIntentsSkipWithoutRuleFacts(t *testing.T) {
	t.Parallel()

	scopeValue := sgReachabilityScope()
	generation := sgReachabilityGeneration()
	// An aws_resource fact, but no aws_security_group_rule fact.
	index := newReducerIntentFactIndex([]facts.Envelope{{FactKind: facts.AWSResourceFactKind, FactID: "r-1"}})

	for _, build := range []func(scope.IngestionScope, scope.ScopeGeneration, *reducerIntentFactIndex) (ReducerIntent, bool){
		buildSecurityGroupEndpointMaterializationReducerIntent,
		buildSecurityGroupRuleMaterializationReducerIntent,
		buildSecurityGroupReachabilityMaterializationReducerIntent,
	} {
		if _, ok := build(scopeValue, generation, index); ok {
			t.Fatal("reachability intent must not fire without a rule fact")
		}
	}
}
