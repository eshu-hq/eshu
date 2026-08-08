// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"

const (
	// ServiceEC2 identifies the regional Amazon Elastic Compute Cloud network
	// topology scan slice.
	ServiceEC2 = "ec2"
)

const (
	// ResourceTypeEC2VPC identifies an EC2 VPC.
	ResourceTypeEC2VPC = awsv1.ResourceTypeEC2VPC
	// ResourceTypeEC2Subnet identifies an EC2 subnet.
	ResourceTypeEC2Subnet = awsv1.ResourceTypeEC2Subnet
	// ResourceTypeEC2SecurityGroup identifies an EC2 security group.
	ResourceTypeEC2SecurityGroup = awsv1.ResourceTypeEC2SecurityGroup
	// ResourceTypeEC2SecurityGroupRule identifies an EC2 security group rule.
	ResourceTypeEC2SecurityGroupRule = awsv1.ResourceTypeEC2SecurityGroupRule
	// ResourceTypeEC2NetworkInterface identifies an EC2 network interface.
	ResourceTypeEC2NetworkInterface = awsv1.ResourceTypeEC2NetworkInterface
	// ResourceTypeEC2Volume identifies an EBS volume observed through the EC2
	// DescribeVolumes API.
	ResourceTypeEC2Volume = awsv1.ResourceTypeEC2Volume
	// ResourceTypeEC2Instance identifies an EC2 instance. It is the join anchor
	// for the metadata-only ec2_instance_posture fact AND (#5448) the identity
	// aws_resource fact the EC2 scanner emits per instance carrying the AMI
	// (ImageId) the instance was launched from. Both facts resolve to the same
	// canonical cloud_resource_uid; the posture fact remains the sole owner of
	// the CloudResource node's base identity/posture properties, and the
	// identity fact only ever augments the node with the disjoint ami_id
	// property (go/internal/reducer/ec2_instance_identity_materialization.go).
	ResourceTypeEC2Instance = awsv1.ResourceTypeEC2Instance
	// ResourceTypeEC2AMI identifies an EC2 AMI (machine image), both as a
	// relationship target (#5448) and, since #5717, as the resource_type of
	// the AMI's own aws_resource identity fact (amiResourceObservation in
	// go/internal/collector/awscloud/services/ec2/ami_identity.go). The AMI
	// materializes as an ordinary CloudResource node — no dedicated node/edge
	// writer, no new graph label — through the SAME generic AWS resource
	// materialization and relationship-edge-join path every other resource
	// type uses (go/internal/reducer/aws_resource_materialization.go,
	// aws_relationship_join.go). It carries only identity: the EC2 scanner
	// reads no DescribeImages data, so `name` carries the bare AMI id and no
	// human-readable name, state, owner, or creation-date metadata is available
	// this increment (a documented scope boundary, not
	// a bug — enrichment via DescribeImages is a separate, costed change).
	ResourceTypeEC2AMI = awsv1.ResourceTypeEC2AMI
)

const (
	// RelationshipEC2SubnetInVPC records subnet membership in a VPC.
	RelationshipEC2SubnetInVPC = "ec2_subnet_in_vpc"
	// RelationshipEC2SecurityGroupInVPC records security group membership in a
	// VPC.
	RelationshipEC2SecurityGroupInVPC = "ec2_security_group_in_vpc"
	// RelationshipEC2SecurityGroupHasRule records a security group child rule.
	RelationshipEC2SecurityGroupHasRule = "ec2_security_group_has_rule"
	// RelationshipEC2NetworkInterfaceInSubnet records ENI placement in a
	// subnet.
	RelationshipEC2NetworkInterfaceInSubnet = "ec2_network_interface_in_subnet"
	// RelationshipEC2NetworkInterfaceInVPC records ENI placement in a VPC.
	RelationshipEC2NetworkInterfaceInVPC = "ec2_network_interface_in_vpc"
	// RelationshipEC2NetworkInterfaceUsesSecurityGroup records security group
	// attachment to an ENI.
	RelationshipEC2NetworkInterfaceUsesSecurityGroup = "ec2_network_interface_uses_security_group"
	// RelationshipEC2NetworkInterfaceAttachedToResource records ENI attachment
	// evidence without emitting the attached resource as an inventory fact.
	RelationshipEC2NetworkInterfaceAttachedToResource = "ec2_network_interface_attached_to_resource"
	// RelationshipEC2VolumeUsesKMSKey records the KMS key AWS reports for EBS
	// volume encryption.
	RelationshipEC2VolumeUsesKMSKey = "ec2_volume_uses_kms_key"
	// RelationshipEC2InstanceUsesAMI records the AMI (ImageId) an EC2 instance
	// was launched from (#5448). The target type is ResourceTypeEC2AMI, which
	// the AMI's own aws_resource identity fact (#5717) now materializes as a
	// CloudResource node, so this relationship's edge projection resolves
	// (see ResourceTypeEC2AMI doc).
	RelationshipEC2InstanceUsesAMI = "ec2_instance_uses_ami"
)
