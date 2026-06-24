// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceVerifiedAccess identifies the regional Amazon Verified Access
	// metadata-only scan slice. The scanner reads Verified Access control-plane
	// metadata through the EC2 management API
	// (DescribeVerifiedAccessInstances, DescribeVerifiedAccessGroups,
	// DescribeVerifiedAccessEndpoints, DescribeVerifiedAccessTrustProviders) and
	// never reads or persists trust-provider client secrets, policy bodies, or
	// any data-plane payload, and never mutates Verified Access state. Although
	// Verified Access ships under the EC2 SDK, it is its own service kind with
	// its own ResourceType constants, distinct from the core `ec2` scanner.
	ServiceVerifiedAccess = "verifiedaccess"
)

const (
	// ResourceTypeVerifiedAccessInstance identifies an Amazon Verified Access
	// instance metadata resource. The scanner emits identity, FIPS-enabled flag,
	// the customer-managed-KMS-key encryption flag, attached trust-provider
	// identifiers, and lifecycle timestamps only.
	ResourceTypeVerifiedAccessInstance = "aws_verifiedaccess_instance"
	// ResourceTypeVerifiedAccessGroup identifies an Amazon Verified Access group
	// metadata resource. The scanner emits identity, owning account, parent
	// instance identity, the customer-managed-KMS-key encryption flag, and
	// lifecycle timestamps only. Group policy documents stay outside the
	// contract.
	ResourceTypeVerifiedAccessGroup = "aws_verifiedaccess_group"
	// ResourceTypeVerifiedAccessEndpoint identifies an Amazon Verified Access
	// endpoint metadata resource. The scanner emits identity, endpoint and
	// attachment type, application/endpoint domains, status, parent group and
	// instance identities, attached subnet/security-group/ACM-certificate
	// references, and lifecycle timestamps only. Endpoint policy documents stay
	// outside the contract.
	ResourceTypeVerifiedAccessEndpoint = "aws_verifiedaccess_endpoint"
	// ResourceTypeVerifiedAccessTrustProvider identifies an Amazon Verified
	// Access trust provider metadata resource. The scanner emits identity, the
	// trust-provider/user-trust-provider/device-trust-provider types, policy
	// reference name, the OIDC issuer reference, and lifecycle timestamps only.
	// OIDC client identifiers and client secrets are never read or persisted.
	ResourceTypeVerifiedAccessTrustProvider = "aws_verifiedaccess_trust_provider"
)

const (
	// RelationshipVerifiedAccessGroupInInstance records a Verified Access group's
	// membership in its parent instance. The target is keyed by the instance ARN
	// the instance node publishes so the edge joins that node exactly.
	RelationshipVerifiedAccessGroupInInstance = "verifiedaccess_group_in_instance"
	// RelationshipVerifiedAccessEndpointInGroup records a Verified Access
	// endpoint's membership in its parent group. The target is keyed by the group
	// ARN the group node publishes.
	RelationshipVerifiedAccessEndpointInGroup = "verifiedaccess_endpoint_in_group"
	// RelationshipVerifiedAccessInstanceUsesTrustProvider records a Verified
	// Access instance's attachment to a trust provider. The target is keyed by
	// the trust-provider ARN the trust-provider node publishes.
	RelationshipVerifiedAccessInstanceUsesTrustProvider = "verifiedaccess_instance_uses_trust_provider"
	// RelationshipVerifiedAccessEndpointUsesSubnet records a Verified Access
	// endpoint's reported VPC subnet placement. The target is the EC2-owned
	// aws_ec2_subnet identity keyed by the bare subnet id (subnet-...).
	RelationshipVerifiedAccessEndpointUsesSubnet = "verifiedaccess_endpoint_uses_subnet"
	// RelationshipVerifiedAccessEndpointUsesSecurityGroup records a Verified
	// Access endpoint's reported security-group attachment. The target is the
	// EC2-owned aws_ec2_security_group identity keyed by the bare group id
	// (sg-...).
	RelationshipVerifiedAccessEndpointUsesSecurityGroup = "verifiedaccess_endpoint_uses_security_group"
	// RelationshipVerifiedAccessEndpointUsesACMCertificate records a Verified
	// Access endpoint's reported public TLS certificate. The target is the
	// aws_acm_certificate identity keyed by the certificate ARN the ACM scanner
	// publishes.
	RelationshipVerifiedAccessEndpointUsesACMCertificate = "verifiedaccess_endpoint_uses_acm_certificate"
)
