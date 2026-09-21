// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceShield identifies the AWS Shield Advanced metadata scan slice.
	// Shield is a global service: one claim per account observes every
	// protection and the account subscription summary regardless of region.
	ServiceShield = "shield"
)

const (
	// ResourceTypeShieldProtection identifies an AWS Shield Advanced protection
	// metadata resource. The protection's resource_id is its protection ARN, and
	// the resource carries the protected resource ARN as an attribute so the
	// protection-to-protected-resource edge can join the protected node.
	ResourceTypeShieldProtection = "aws_shield_protection"
	// ResourceTypeShieldSubscription identifies the per-account AWS Shield
	// Advanced subscription summary resource. It carries subscription state and
	// auto-renew metadata only; no billing detail is read or persisted.
	ResourceTypeShieldSubscription = "aws_shield_subscription"
)

const (
	// RelationshipShieldProtectionProtectsResource records a Shield Advanced
	// protection association to the resource it protects, keyed by the protected
	// resource ARN reported by AWS. The edge's target_type is derived from the
	// protected ARN's service segment (ELBv2 load balancer, CloudFront
	// distribution, EIP, Route 53 hosted zone, or Global Accelerator
	// accelerator), and its target_resource_id matches how that target scanner
	// publishes its resource_id.
	RelationshipShieldProtectionProtectsResource = "shield_protection_protects_resource"
)
