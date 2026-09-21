// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDAX identifies the regional Amazon DynamoDB Accelerator (DAX)
	// metadata-only scan slice. The scanner reads cluster, subnet-group, and
	// parameter-group control-plane metadata through the DAX management APIs
	// (DescribeClusters, DescribeSubnetGroups, DescribeParameterGroups, and
	// ListTags) and never reads cached DynamoDB item data, query results, or node
	// endpoint payloads, and never mutates DAX state.
	ServiceDAX = "dax"
)

const (
	// ResourceTypeDAXCluster identifies an Amazon DAX cluster metadata resource.
	// The scanner emits identity, node type, node counts, status, network type,
	// endpoint encryption type, the assumed IAM role ARN, parameter-group and
	// subnet-group names, security-group memberships, and the server-side
	// encryption status only. Per-node endpoint addresses are recorded as plain
	// connection metadata and never as secrets; cached DynamoDB items and query
	// results stay outside the contract.
	ResourceTypeDAXCluster = "aws_dax_cluster"
	// ResourceTypeDAXSubnetGroup identifies an Amazon DAX subnet group metadata
	// resource. DAX subnet groups have no ARN, so the scanner keys the resource by
	// name and emits the VPC id, member subnet ids, and description only.
	ResourceTypeDAXSubnetGroup = "aws_dax_subnet_group"
	// ResourceTypeDAXParameterGroup identifies an Amazon DAX parameter group
	// metadata resource. DAX parameter groups expose only a name and description
	// through DescribeParameterGroups; the scanner persists those and never reads
	// individual parameter values, which can reveal operational posture.
	ResourceTypeDAXParameterGroup = "aws_dax_parameter_group"
)

const (
	// RelationshipDAXClusterInSubnetGroup records a DAX cluster's placement in its
	// subnet group. DAX subnet groups have no ARN, so the target is keyed by the
	// subnet-group name the subnet-group resource publishes as its resource_id.
	RelationshipDAXClusterInSubnetGroup = "dax_cluster_in_subnet_group"
	// RelationshipDAXClusterUsesSecurityGroup records a DAX cluster's reported
	// VPC security-group membership. The target is keyed by the bare security
	// group id (sg-...), matching how the EC2 scanner publishes a security group.
	RelationshipDAXClusterUsesSecurityGroup = "dax_cluster_uses_security_group"
	// RelationshipDAXClusterAssumesIAMRole records the IAM role a DAX cluster
	// assumes at runtime to access DynamoDB. The target is keyed by the role ARN,
	// matching how the IAM scanner publishes a role.
	RelationshipDAXClusterAssumesIAMRole = "dax_cluster_assumes_iam_role"
	// RelationshipDAXSubnetGroupInVPC records a DAX subnet group's VPC. The target
	// is keyed by the bare VPC id (vpc-...), matching how the EC2 scanner
	// publishes a VPC.
	RelationshipDAXSubnetGroupInVPC = "dax_subnet_group_in_vpc"
	// RelationshipDAXSubnetGroupHasSubnet records a member subnet of a DAX subnet
	// group. The target is keyed by the bare subnet id (subnet-...), matching how
	// the EC2 scanner publishes a subnet.
	RelationshipDAXSubnetGroupHasSubnet = "dax_subnet_group_has_subnet"
)
