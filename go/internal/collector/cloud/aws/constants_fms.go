// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceFMS identifies the AWS Firewall Manager (FMS) metadata scan slice.
	// FMS is an organization-wide control plane reachable only from the FMS
	// administrator account, so one claim scans the administrator account's
	// policy fleet rather than a per-account resource set.
	ServiceFMS = "fms"
)

const (
	// ResourceTypeFMSPolicy identifies an AWS Firewall Manager policy metadata
	// resource. The resource carries policy identity, the governing security
	// service type, the in-scope resource type label, and the remediation flag;
	// the policy rule payload (SecurityServicePolicyData managed service data)
	// is never read or persisted.
	ResourceTypeFMSPolicy = "aws_fms_policy"
)

const (
	// RelationshipFMSPolicyAppliesToAccount records that a Firewall Manager
	// policy applies to (is evaluated against) an Organizations member account.
	// The edge keys on the bare 12-digit member account id, matching the
	// resource_id the organizations scanner publishes for an
	// aws_organizations_account node, so the edge joins instead of dangling.
	RelationshipFMSPolicyAppliesToAccount = "fms_policy_applies_to_account"
)
