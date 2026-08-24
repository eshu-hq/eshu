// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package security

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// securityGroupReachabilityAcceptanceUnit is the shared entity key all three
// security-group reachability domains anchor on. It intentionally matches the AWS
// resource materialization intent ("aws_resource_materialization:<scope>") so the
// rule, endpoint, and SG-node readiness phases — and the edge domain's gate that
// joins all three — resolve the exact same GraphProjectionPhaseKey acceptance
// unit. Diverging keys here would make the triple-gate join nothing and the edge
// slice would never drain.
func securityGroupReachabilityAcceptanceUnit(scopeID string) string {
	return "aws_resource_materialization:" + scopeID
}

// BuildSecurityGroupEndpointMaterializationReducerIntent enqueues the CidrBlock /
// PrefixList endpoint node materialization intent (issue #1135 PR2a) when any
// aws_security_group_rule fact is present. PR2a shipped the handler, schema, and
// readiness phase but no projector trigger, so without this the endpoint nodes
// never materialize and the reachability edge gate blocks forever. The intent is
// anchored to the first rule fact for a stable reducer claim across reprojections.
func BuildSecurityGroupEndpointMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	return securityGroupReachabilityIntentForDomain(
		scopeID,
		generationID,
		lookup,
		reducer.DomainSecurityGroupCidrMaterialization,
		"aws security group rule facts observed (endpoint nodes)",
	)
}

// BuildSecurityGroupRuleMaterializationReducerIntent enqueues the
// :SecurityGroupRule node materialization intent (issue #1135 PR2b Option D) when
// any aws_security_group_rule fact is present. The node domain publishes the
// security_group_rule_uid readiness phase the edge domain gates on.
func BuildSecurityGroupRuleMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	return securityGroupReachabilityIntentForDomain(
		scopeID,
		generationID,
		lookup,
		reducer.DomainSecurityGroupRuleMaterialization,
		"aws security group rule facts observed (rule nodes)",
	)
}

// BuildSecurityGroupReachabilityMaterializationReducerIntent enqueues the
// reachability edge projection intent (issue #1135 PR2b Option D) when any
// aws_security_group_rule fact is present. The edge handler gates on the rule,
// endpoint, and SG-node canonical-nodes phases before resolving any edge.
func BuildSecurityGroupReachabilityMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	return securityGroupReachabilityIntentForDomain(
		scopeID,
		generationID,
		lookup,
		reducer.DomainSecurityGroupReachabilityMaterialization,
		"aws security group rule facts observed (reachability edges)",
	)
}

// securityGroupReachabilityIntentForDomain builds one reachability intent for the
// given domain, anchored to the first aws_security_group_rule fact in the
// generation. All three domains share the trigger (a rule fact) and the
// acceptance unit, so a single helper keeps them in lockstep.
func securityGroupReachabilityIntentForDomain(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
	domain reducer.Domain,
	reason string,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AWSSecurityGroupRuleFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       domain,
		EntityKey:    securityGroupReachabilityAcceptanceUnit(scopeID),
		Reason:       reason,
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
