// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package secgroup materializes aws_security_group_rule facts into the
// Option D network-reachability graph: CidrBlock/PrefixList endpoint nodes,
// port-precise :SecurityGroupRule nodes, the SecurityGroup -> rule
// ALLOWS_INGRESS/EGRESS edges, and the rule -[:TO]-> endpoint edges (issue
// #1135).
//
// The three domains are additive and gate on each other's canonical-nodes
// readiness rather than on graph round trips: the endpoint domain publishes
// [gpphase.KeyspaceSecurityGroupEndpointUID], the rule-node domain publishes
// [gpphase.KeyspaceSecurityGroupRuleUID], and the reachability edge domain
// gates on both of those plus [gpphase.KeyspaceCloudResourceUID] before
// writing a single edge. A miss on any of the three canonical-nodes phases is
// retryable ([SecurityGroupReachabilityNodesNotReadyFailureClass]), never a
// fabricated edge against a node set that has not committed yet.
//
// A rule's SG anchor and its source endpoint (CIDR, managed prefix list, or a
// referenced security group) are resolved against a bounded in-memory
// CloudResource join index built from the scope generation's aws_resource
// facts, mirroring the AWS relationship edge join (#805). A rule whose anchor
// or endpoint is not a materialized node in this scope produces no node and no
// edges -- counted in a skip tally, never dangled or fabricated.
//
// Every extractor is deduplicated by node/edge identity and sorted by uid, so
// a batched write is byte-stable and idempotent across retries and
// reprojections. A malformed aws_security_group_rule fact is quarantined
// per-fact (input_invalid) while every valid fact in the same batch still
// projects.
//
// The exported surface is [CidrMaterializationDomainDefinition],
// [SecurityGroupEndpointNodeWriter], [SecurityGroupCidrMaterializationHandler],
// [ExtractSecurityGroupEndpointRows], [RuleMaterializationDomainDefinition],
// [SecurityGroupRuleNodeWriter], [SecurityGroupRuleMaterializationHandler],
// [ReachabilityMaterializationDomainDefinition],
// [SecurityGroupReachabilityWriter],
// [SecurityGroupReachabilityMaterializationHandler],
// [SecurityGroupReachabilityResult], [ExtractSecurityGroupReachability],
// [SecurityGroupReachabilityEvidenceSource], and
// [SecurityGroupReachabilityNodesNotReadyFailureClass]. This package never
// imports internal/reducer.
package secgroup
