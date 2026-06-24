// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendSecurityGroupEndpointDomain registers the additive security-group
// endpoint node materialization domain (issue #1135 PR2a) when its node writer
// and the fact loader are both wired. It is additive — registering the domain
// without a node writer would silently drop every aws_security_group_rule
// endpoint before it reached the graph — so the registration is gated on both
// dependencies, mirroring the AWS resource and Kubernetes workload node domains.
func appendSecurityGroupEndpointDomain(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.SecurityGroupEndpointNodeWriter == nil {
		return definitions
	}
	endpoints := securityGroupCidrMaterializationDomainDefinition()
	endpoints.Handler = SecurityGroupCidrMaterializationHandler{
		FactLoader:     handlers.FactLoader,
		NodeWriter:     handlers.SecurityGroupEndpointNodeWriter,
		PhasePublisher: handlers.GraphProjectionPhasePublisher,
		Instruments:    handlers.Instruments,
	}
	return append(definitions, endpoints)
}

// appendSecurityGroupReachabilityDomains registers the additive :SecurityGroupRule
// node materialization domain and the Option D reachability edge domain (issue
// #1135 PR2b) when their writers and the fact loader are wired. Both are additive
// for the same reason as the endpoint domain: registering either without its
// writer would silently drop graph truth. The rule-node domain publishes the
// security_group_rule_uid readiness phase; the edge domain gates on it (plus the
// endpoint and cloud-resource node phases) through ReadinessLookup, mirroring the
// AWS resource->relationship node/edge split (#805).
func appendSecurityGroupReachabilityDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader != nil && handlers.SecurityGroupRuleNodeWriter != nil {
		ruleNodes := securityGroupRuleMaterializationDomainDefinition()
		ruleNodes.Handler = SecurityGroupRuleMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.SecurityGroupRuleNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, ruleNodes)
	}
	if handlers.FactLoader != nil && handlers.SecurityGroupReachabilityWriter != nil {
		edges := securityGroupReachabilityMaterializationDomainDefinition()
		edges.Handler = SecurityGroupReachabilityMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.SecurityGroupReachabilityWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, edges)
	}
	return definitions
}
