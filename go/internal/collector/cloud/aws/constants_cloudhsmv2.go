// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCloudHSMV2 identifies the regional AWS CloudHSM v2 metadata-only
	// scan slice. The scanner reads control-plane metadata through the
	// CloudHSM v2 management APIs (DescribeClusters, DescribeBackups, ListTags)
	// and never reads, writes, or persists any cryptographic key material,
	// certificate private bodies, certificate signing requests, or the
	// cluster's Pre-Crypto Officer password.
	ServiceCloudHSMV2 = "cloudhsmv2"
)

const (
	// ResourceTypeCloudHSMV2Cluster identifies an AWS CloudHSM v2 cluster
	// metadata resource. The scanner emits identity, state, HSM type, mode,
	// network type, backup-policy presence, embedded HSM ENI metadata, and
	// certificate presence flags only. It never emits certificate PEM bodies,
	// the cluster CSR body, or the Pre-Crypto Officer password.
	ResourceTypeCloudHSMV2Cluster = "aws_cloudhsmv2_cluster"
	// ResourceTypeCloudHSMV2Backup identifies an AWS CloudHSM v2 backup
	// metadata resource. The scanner emits identity, state, timestamps, source
	// backup/cluster references, and never-expires/HSM-type metadata only. The
	// backup never carries key material.
	ResourceTypeCloudHSMV2Backup = "aws_cloudhsmv2_backup"
)

const (
	// RelationshipCloudHSMV2ClusterInVPC records a CloudHSM v2 cluster's parent
	// VPC. The target is keyed by the bare VPC id (vpc-…) the EC2 scanner
	// publishes as a VPC node's resource_id.
	RelationshipCloudHSMV2ClusterInVPC = "cloudhsmv2_cluster_in_vpc"
	// RelationshipCloudHSMV2ClusterInSubnet records a CloudHSM v2 cluster's
	// presence in one VPC subnet drawn from its availability-zone-to-subnet
	// mapping. The target is keyed by the bare subnet id (subnet-…) the EC2
	// scanner publishes as a subnet node's resource_id.
	RelationshipCloudHSMV2ClusterInSubnet = "cloudhsmv2_cluster_in_subnet"
	// RelationshipCloudHSMV2ClusterUsesSecurityGroup records a CloudHSM v2
	// cluster's reported AWS-managed cluster security group. The target is keyed
	// by the bare security-group id (sg-…) the EC2 scanner publishes as a
	// security-group node's resource_id.
	RelationshipCloudHSMV2ClusterUsesSecurityGroup = "cloudhsmv2_cluster_uses_security_group"
	// RelationshipCloudHSMV2BackupOfCluster records a CloudHSM v2 backup's
	// source cluster. The target is keyed by the bare cluster id the CloudHSM v2
	// cluster node publishes as its resource_id.
	RelationshipCloudHSMV2BackupOfCluster = "cloudhsmv2_backup_of_cluster"
)
