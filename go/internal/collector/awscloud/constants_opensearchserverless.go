// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceOpenSearchServerless identifies the regional Amazon OpenSearch
	// Serverless (aoss) metadata-only scan slice. The scanner reads control-plane
	// describe/list APIs (ListCollections/BatchGetCollection,
	// ListSecurityPolicies/GetSecurityPolicy,
	// ListVpcEndpoints/BatchGetVpcEndpoint, ListTagsForResource) and emits
	// collection, security-policy, and managed VPC-endpoint metadata. It never
	// reads the OpenSearch HTTP data plane (index, search, bulk, document APIs),
	// never persists access-policy or security-policy document bodies, and never
	// mutates Serverless state.
	//
	// It is a distinct service_kind from ServiceOpenSearch: the opensearch
	// scanner surfaces Serverless collections only as a side slice of the broader
	// OpenSearch Service domain scan, while this scanner is the first-class
	// Serverless owner that additionally emits security policies as resources and
	// the managed VPC endpoint's VPC/subnet/security-group network edges, mirroring
	// how the firehose scanner coexists with the kinesis Firehose side slice.
	ServiceOpenSearchServerless = "opensearchserverless"
)

const (
	// ResourceTypeOpenSearchServerlessAOSSCollection identifies an Amazon
	// OpenSearch Serverless collection (aoss). The scanner emits identity, type,
	// status, and standby-replicas metadata only. Collection endpoints, dashboard
	// endpoints, saved-object bodies, and indexed data are never read or
	// persisted. The type string is intentionally distinct from the opensearch
	// scanner's aws_opensearch_serverless_collection so the two slices do not
	// collide on graph identity.
	ResourceTypeOpenSearchServerlessAOSSCollection = "aws_opensearchserverless_collection"
	// ResourceTypeOpenSearchServerlessSecurityPolicy identifies an Amazon
	// OpenSearch Serverless security policy summary (name, type, version,
	// description, timestamps). The policy document body is never persisted; only
	// the KMS key reference parsed from an encryption policy is surfaced as a
	// graph edge.
	ResourceTypeOpenSearchServerlessSecurityPolicy = "aws_opensearchserverless_security_policy"
	// ResourceTypeOpenSearchServerlessAOSSVPCEndpoint identifies an Amazon
	// OpenSearch Serverless managed interface VPC endpoint (name, id, status, VPC,
	// subnet, and security-group references). The type string is intentionally
	// distinct from the opensearch scanner's aws_opensearch_serverless_vpc_endpoint.
	ResourceTypeOpenSearchServerlessAOSSVPCEndpoint = "aws_opensearchserverless_vpc_endpoint"
)

const (
	// RelationshipOpenSearchServerlessCollectionUsesKMSKey records the KMS
	// customer-managed encryption key an OpenSearch Serverless collection is
	// assigned through its matching encryption security policy. The target is the
	// KMS-owned aws_kms_key identity keyed by the key ARN the encryption policy
	// document reports. AWS-owned-key policies emit no edge.
	RelationshipOpenSearchServerlessCollectionUsesKMSKey = "opensearchserverless_collection_uses_kms_key"
	// RelationshipOpenSearchServerlessVPCEndpointInVPC records an OpenSearch
	// Serverless managed VPC endpoint's reported VPC placement. The target is the
	// EC2-owned aws_ec2_vpc identity keyed by the bare vpc-… id.
	RelationshipOpenSearchServerlessVPCEndpointInVPC = "opensearchserverless_vpc_endpoint_in_vpc"
	// RelationshipOpenSearchServerlessVPCEndpointInSubnet records an OpenSearch
	// Serverless managed VPC endpoint's reported subnet placement. The target is
	// the EC2-owned aws_ec2_subnet identity keyed by the bare subnet-… id.
	RelationshipOpenSearchServerlessVPCEndpointInSubnet = "opensearchserverless_vpc_endpoint_in_subnet"
	// RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup records an
	// OpenSearch Serverless managed VPC endpoint's reported security-group
	// attachment. The target is the EC2-owned aws_ec2_security_group identity
	// keyed by the bare sg-… id.
	RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup = "opensearchserverless_vpc_endpoint_uses_security_group"
)
