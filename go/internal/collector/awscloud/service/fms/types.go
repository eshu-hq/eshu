// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fms

import "context"

// Client is the AWS Firewall Manager read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned metadata
// records. The adapter must never return policy rule payloads (the
// SecurityServicePolicyData managed service data document) and must never call
// an FMS mutation API.
type Client interface {
	// ListPolicies returns the administrator account's Firewall Manager policy
	// summaries, including the governing security service type, the in-scope
	// resource type label, and the remediation flag. The policy rule body is
	// never returned.
	ListPolicies(context.Context) ([]Policy, error)
	// ListPolicyMemberAccounts returns the bare 12-digit Organizations member
	// account ids a policy is evaluated against, resolved from the policy
	// compliance status. The account ids are deduplicated and order-independent
	// so a synthesized relationship identity never keys on API order.
	ListPolicyMemberAccounts(ctx context.Context, policyID string) ([]string, error)
}

// Policy is the scanner-owned representation of one Firewall Manager policy. It
// carries policy identity, the governing security service type, the in-scope
// AWS resource type label, and the remediation flag. The policy rule payload
// (the managed service data document inside SecurityServicePolicyData) is
// intentionally outside this contract and is never read.
type Policy struct {
	// ARN is the policy ARN exactly as AWS reports it. The scanner derives every
	// downstream identity from this partition-bearing value and never
	// synthesizes an ARN with a hardcoded partition.
	ARN string
	// ID is the Firewall Manager policy id.
	ID string
	// Name is the Firewall Manager policy name.
	Name string
	// SecurityServiceType is the AWS security service the policy uses to protect
	// resources (WAF, WAFV2, SECURITY_GROUPS_COMMON, NETWORK_FIREWALL,
	// SHIELD_ADVANCED, DNS_FIREWALL, and the other FMS security service types).
	SecurityServiceType string
	// ResourceType is the in-scope AWS resource type the policy governs, in the
	// CloudFormation resource type format AWS reports (for example
	// AWS::ElasticLoadBalancingV2::LoadBalancer or AWS::EC2::VPC).
	ResourceType string
	// RemediationEnabled reports whether the policy automatically applies its
	// protection to new in-scope resources.
	RemediationEnabled bool
	// DeleteUnusedFMManagedResources reports whether Firewall Manager removes
	// protections and managed resources when a resource leaves policy scope.
	DeleteUnusedFMManagedResources bool
	// PolicyStatus is the administrator-scope status AWS reports (ACTIVE or
	// OUT_OF_ADMIN_SCOPE).
	PolicyStatus string
}
