// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package security

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	sgReachabilityScopeID      = "aws:111122223333:us-east-1"
	sgReachabilityGenerationID = "gen-1"
)

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

	lookup := projectorintent.NewFactLookup([]facts.Envelope{sgRuleFactEnvelope()})

	cases := []struct {
		name    string
		build   intentBuilder
		wantDom reducer.Domain
	}{
		{"endpoint", BuildSecurityGroupEndpointMaterializationReducerIntent, reducer.DomainSecurityGroupCidrMaterialization},
		{"rule_node", BuildSecurityGroupRuleMaterializationReducerIntent, reducer.DomainSecurityGroupRuleMaterialization},
		{"edge", BuildSecurityGroupReachabilityMaterializationReducerIntent, reducer.DomainSecurityGroupReachabilityMaterialization},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			intent, ok := tc.build(sgReachabilityScopeID, sgReachabilityGenerationID, lookup)
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

	// An aws_resource fact, but no aws_security_group_rule fact.
	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: facts.AWSResourceFactKind, FactID: "r-1"}})

	for _, build := range []intentBuilder{
		BuildSecurityGroupEndpointMaterializationReducerIntent,
		BuildSecurityGroupRuleMaterializationReducerIntent,
		BuildSecurityGroupReachabilityMaterializationReducerIntent,
	} {
		if _, ok := build(sgReachabilityScopeID, sgReachabilityGenerationID, lookup); ok {
			t.Fatal("reachability intent must not fire without a rule fact")
		}
	}
}
