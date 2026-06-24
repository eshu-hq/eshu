// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceMSK identifies the regional Amazon Managed Streaming for Apache
	// Kafka metadata-only scan slice.
	ServiceMSK = "msk"
)

const (
	// ResourceTypeMSKCluster identifies an MSK provisioned or serverless
	// cluster metadata resource.
	ResourceTypeMSKCluster = "aws_msk_cluster"
	// ResourceTypeMSKConfiguration identifies an MSK broker configuration
	// metadata resource. The scanner emits configuration identifiers and the
	// latest revision summary; raw server.properties bodies are never
	// persisted.
	ResourceTypeMSKConfiguration = "aws_msk_configuration"
	// ResourceTypeMSKReplicator identifies an MSK Replicator metadata
	// resource.
	ResourceTypeMSKReplicator = "aws_msk_replicator"
)

const (
	// RelationshipMSKClusterInVPC records an MSK cluster's reported VPC
	// placement when AWS reports the VPC identity.
	RelationshipMSKClusterInVPC = "msk_cluster_in_vpc"
	// RelationshipMSKClusterUsesSubnet records an MSK cluster's reported
	// client subnet placement.
	RelationshipMSKClusterUsesSubnet = "msk_cluster_uses_subnet"
	// RelationshipMSKClusterUsesSecurityGroup records an MSK cluster's
	// reported security group attachment.
	RelationshipMSKClusterUsesSecurityGroup = "msk_cluster_uses_security_group"
	// RelationshipMSKClusterUsesKMSKey records an MSK cluster's reported
	// data-volume KMS key dependency.
	RelationshipMSKClusterUsesKMSKey = "msk_cluster_uses_kms_key"
	// RelationshipMSKClusterUsesIAMRole records an MSK cluster's reported IAM
	// role dependency when AWS reports an ARN-shaped role identity.
	RelationshipMSKClusterUsesIAMRole = "msk_cluster_uses_iam_role"
	// RelationshipMSKClusterUsesConfiguration records the MSK broker
	// configuration currently applied to an MSK cluster.
	RelationshipMSKClusterUsesConfiguration = "msk_cluster_uses_configuration"
	// RelationshipMSKReplicatorUsesIAMRole records an MSK Replicator's
	// reported service-execution IAM role dependency.
	RelationshipMSKReplicatorUsesIAMRole = "msk_replicator_uses_iam_role"
)
