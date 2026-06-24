// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceLicenseManager identifies the regional AWS License Manager
	// metadata-only scan slice. The scanner reads license-configuration
	// control-plane metadata through the License Manager management APIs
	// (ListLicenseConfigurations, ListAssociationsForLicenseConfiguration,
	// ListTagsForResource) and never grants, checks out, mutates, or otherwise
	// changes license state, and never reads license entitlement tokens or
	// usage records.
	ServiceLicenseManager = "licensemanager"
)

const (
	// ResourceTypeLicenseManagerConfiguration identifies an AWS License Manager
	// license configuration metadata resource. The scanner emits identity, the
	// license counting dimension, the configured and consumed license counts,
	// the hard-limit and status flags, and the count of associated resources
	// only. License rules and product-information match expressions are recorded
	// as structural metadata; no entitlement token or usage record is read.
	ResourceTypeLicenseManagerConfiguration = "aws_license_manager_configuration"
)

const (
	// RelationshipLicenseManagerConfigurationAppliesToInstance records that a
	// License Manager license configuration is associated with an EC2 instance.
	// The edge targets the EC2 instance by its bare instance id (i-...), which is
	// how EC2 instance nodes are keyed, so the edge joins the instance node
	// instead of dangling.
	RelationshipLicenseManagerConfigurationAppliesToInstance = "license_manager_configuration_applies_to_instance"
)
