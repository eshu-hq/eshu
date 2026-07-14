// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// securityGroupReachabilityAcceptanceUnit is the shared entity key all three
// security-group reachability domains anchor on. It intentionally matches the AWS
// resource materialization intent ("aws_resource_materialization:<scope>") so the
// rule, endpoint, and SG-node readiness phases — and the edge domain's gate that
// joins all three — resolve the exact same GraphProjectionPhaseKey acceptance
// unit. Diverging keys here would make the triple-gate join nothing and the edge
// slice would never drain.
func securityGroupReachabilityAcceptanceUnit(scopeValue scope.IngestionScope) string {
	return "aws_resource_materialization:" + scopeValue.ScopeID
}

// buildSecurityGroupEndpointMaterializationReducerIntent enqueues the CidrBlock /
// PrefixList endpoint node materialization intent (issue #1135 PR2a) when any
// aws_security_group_rule fact is present. PR2a shipped the handler, schema, and
// readiness phase but no projector trigger, so without this the endpoint nodes
// never materialize and the reachability edge gate blocks forever. The intent is
// anchored to the first rule fact for a stable reducer claim across reprojections.
func buildSecurityGroupEndpointMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	return securityGroupReachabilityIntentForDomain(
		scopeValue,
		generation,
		index,
		reducer.DomainSecurityGroupCidrMaterialization,
		"aws security group rule facts observed (endpoint nodes)",
	)
}

// buildSecurityGroupRuleMaterializationReducerIntent enqueues the
// :SecurityGroupRule node materialization intent (issue #1135 PR2b Option D) when
// any aws_security_group_rule fact is present. The node domain publishes the
// security_group_rule_uid readiness phase the edge domain gates on.
func buildSecurityGroupRuleMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	return securityGroupReachabilityIntentForDomain(
		scopeValue,
		generation,
		index,
		reducer.DomainSecurityGroupRuleMaterialization,
		"aws security group rule facts observed (rule nodes)",
	)
}

// buildSecurityGroupReachabilityMaterializationReducerIntent enqueues the
// reachability edge projection intent (issue #1135 PR2b Option D) when any
// aws_security_group_rule fact is present. The edge handler gates on the rule,
// endpoint, and SG-node canonical-nodes phases before resolving any edge.
func buildSecurityGroupReachabilityMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	return securityGroupReachabilityIntentForDomain(
		scopeValue,
		generation,
		index,
		reducer.DomainSecurityGroupReachabilityMaterialization,
		"aws security group rule facts observed (reachability edges)",
	)
}

// securityGroupReachabilityIntentForDomain builds one reachability intent for the
// given domain, anchored to the first aws_security_group_rule fact in the
// generation. All three domains share the trigger (a rule fact) and the
// acceptance unit, so a single helper keeps them in lockstep.
func securityGroupReachabilityIntentForDomain(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
	domain reducer.Domain,
	reason string,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.AWSSecurityGroupRuleFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       domain,
		EntityKey:    securityGroupReachabilityAcceptanceUnit(scopeValue),
		Reason:       reason,
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
