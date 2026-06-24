// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceOpenSearch identifies the regional Amazon OpenSearch metadata scan
	// slice. It covers OpenSearch Service provisioned domains, OpenSearch custom
	// packages, and OpenSearch Serverless collections, security configurations,
	// and managed VPC endpoints under one service_kind.
	ServiceOpenSearch = "opensearch"
)

const (
	// ResourceTypeOpenSearchDomain identifies an OpenSearch Service provisioned
	// domain. Domain endpoint contents and master user passwords are never
	// persisted.
	ResourceTypeOpenSearchDomain = "aws_opensearch_domain"
	// ResourceTypeOpenSearchPackage identifies an OpenSearch custom package's
	// metadata (name, type, status); the package body is never persisted.
	ResourceTypeOpenSearchPackage = "aws_opensearch_package"
	// ResourceTypeOpenSearchServerlessCollection identifies an OpenSearch
	// Serverless collection. Collection saved-object bodies and indexed data are
	// never read or persisted.
	ResourceTypeOpenSearchServerlessCollection = "aws_opensearch_serverless_collection"
	// ResourceTypeOpenSearchServerlessSecurityConfig identifies an OpenSearch
	// Serverless security configuration summary (id, type, version); SAML
	// metadata XML and IAM Identity Center secrets stay outside the scan slice.
	ResourceTypeOpenSearchServerlessSecurityConfig = "aws_opensearch_serverless_security_config"
	// ResourceTypeOpenSearchServerlessVPCEndpoint identifies an OpenSearch
	// Serverless managed VPC interface endpoint.
	ResourceTypeOpenSearchServerlessVPCEndpoint = "aws_opensearch_serverless_vpc_endpoint"
)

const (
	// RelationshipOpenSearchDomainInVPC records an OpenSearch Service domain's
	// reported VPC placement. The target is the EC2-owned aws_ec2_vpc identity.
	RelationshipOpenSearchDomainInVPC = "opensearch_domain_in_vpc"
	// RelationshipOpenSearchDomainInSubnet records an OpenSearch Service domain's
	// reported subnet placement. The target is the EC2-owned aws_ec2_subnet
	// identity.
	RelationshipOpenSearchDomainInSubnet = "opensearch_domain_in_subnet"
	// RelationshipOpenSearchDomainUsesSecurityGroup records an OpenSearch Service
	// domain's reported security group. The target is the EC2-owned
	// aws_ec2_security_group identity.
	RelationshipOpenSearchDomainUsesSecurityGroup = "opensearch_domain_uses_security_group"
	// RelationshipOpenSearchDomainUsesKMSKey records an OpenSearch Service
	// domain's reported at-rest encryption KMS key. The target is the KMS-owned
	// aws_kms_key identity.
	RelationshipOpenSearchDomainUsesKMSKey = "opensearch_domain_uses_kms_key"
	// RelationshipOpenSearchDomainUsesIAMRole records an IAM role ARN referenced
	// by an OpenSearch Service domain's master-user mapping or resource access
	// policy. The target is the IAM-owned aws_iam_role identity.
	RelationshipOpenSearchDomainUsesIAMRole = "opensearch_domain_uses_iam_role"
	// RelationshipOpenSearchPackageAssociatedWithDomain records a custom package
	// associated with an OpenSearch Service domain. The target is the
	// OpenSearch-owned aws_opensearch_domain identity.
	RelationshipOpenSearchPackageAssociatedWithDomain = "opensearch_package_associated_with_domain"
	// RelationshipOpenSearchCollectionUsesKMSKey records an OpenSearch Serverless
	// collection's reported at-rest encryption KMS key. The target is the
	// KMS-owned aws_kms_key identity.
	RelationshipOpenSearchCollectionUsesKMSKey = "opensearch_collection_uses_kms_key"
)
