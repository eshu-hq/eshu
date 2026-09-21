// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAppStream identifies the regional Amazon AppStream 2.0 metadata-only
	// scan slice. The scanner reads fleet, stack, image builder, and image
	// control-plane metadata through the AppStream describe/list management APIs
	// (DescribeFleets, DescribeStacks, DescribeImageBuilders, DescribeImages,
	// ListAssociatedStacks, ListAssociatedFleets, ListTagsForResource) and never
	// reads streaming sessions, user data, or session scripts, and never mutates
	// AppStream state.
	ServiceAppStream = "appstream"
)

const (
	// ResourceTypeAppStreamFleet identifies an Amazon AppStream 2.0 fleet
	// metadata resource. The scanner emits identity, fleet type, instance type,
	// lifecycle state, platform, stream view, internet access, and capacity
	// limits only.
	ResourceTypeAppStreamFleet = "aws_appstream_fleet"
	// ResourceTypeAppStreamStack identifies an Amazon AppStream 2.0 stack
	// metadata resource. The scanner emits identity, display name, persistent
	// application-settings enablement, and the S3 bucket names AppStream reports
	// for app-settings and storage connectors only.
	ResourceTypeAppStreamStack = "aws_appstream_stack"
	// ResourceTypeAppStreamImageBuilder identifies an Amazon AppStream 2.0 image
	// builder metadata resource. The scanner emits identity, instance type,
	// lifecycle state, platform, and the base image ARN only.
	ResourceTypeAppStreamImageBuilder = "aws_appstream_image_builder"
	// ResourceTypeAppStreamImage identifies an Amazon AppStream 2.0 image
	// metadata resource. The scanner emits identity, state, visibility, image
	// type, platform, and the base image ARN only; installed applications and
	// image-permission grants stay outside the contract.
	ResourceTypeAppStreamImage = "aws_appstream_image"
)

const (
	// RelationshipAppStreamFleetUsesSubnet records an AppStream fleet's reported
	// VPC subnet dependency. The target is keyed by the bare subnet id
	// (subnet-...), matching how the EC2 scanner publishes its subnet resource_id.
	RelationshipAppStreamFleetUsesSubnet = "appstream_fleet_uses_subnet"
	// RelationshipAppStreamFleetUsesSecurityGroup records an AppStream fleet's
	// reported VPC security group dependency. The target is keyed by the bare
	// security group id (sg-...), matching the EC2 scanner's published id.
	RelationshipAppStreamFleetUsesSecurityGroup = "appstream_fleet_uses_security_group"
	// RelationshipAppStreamFleetUsesIAMRole records an AppStream fleet's applied
	// IAM role. AWS reports a role ARN, which matches how the IAM scanner
	// publishes its role resource_id.
	RelationshipAppStreamFleetUsesIAMRole = "appstream_fleet_uses_iam_role"
	// RelationshipAppStreamFleetUsesImage records an AppStream fleet's source
	// image. AWS reports the image ARN, which matches how this scanner publishes
	// its image node resource_id.
	RelationshipAppStreamFleetUsesImage = "appstream_fleet_uses_image"
	// RelationshipAppStreamFleetAssociatedWithStack records an AppStream
	// fleet-to-stack association reported by ListAssociatedStacks. The target is
	// keyed by the stack node resource_id (stack ARN when available, else name).
	RelationshipAppStreamFleetAssociatedWithStack = "appstream_fleet_associated_with_stack"
	// RelationshipAppStreamImageBuilderUsesSubnet records an AppStream image
	// builder's reported VPC subnet dependency, keyed by the bare subnet id.
	RelationshipAppStreamImageBuilderUsesSubnet = "appstream_image_builder_uses_subnet"
	// RelationshipAppStreamImageBuilderUsesSecurityGroup records an AppStream
	// image builder's reported VPC security group dependency, keyed by the bare
	// security group id.
	RelationshipAppStreamImageBuilderUsesSecurityGroup = "appstream_image_builder_uses_security_group"
	// RelationshipAppStreamImageBuilderUsesIAMRole records an AppStream image
	// builder's applied IAM role, keyed by the reported role ARN.
	RelationshipAppStreamImageBuilderUsesIAMRole = "appstream_image_builder_uses_iam_role"
	// RelationshipAppStreamImageBuilderUsesImage records an AppStream image
	// builder's base image, keyed by the reported image ARN.
	RelationshipAppStreamImageBuilderUsesImage = "appstream_image_builder_uses_image"
	// RelationshipAppStreamStackUsesS3Bucket records an AppStream stack's reported
	// S3 bucket dependency for persistent application settings or a home-folders
	// storage connector. AppStream reports a bucket NAME, so the scanner
	// synthesizes the partition-aware bucket ARN (arn:<partition>:s3:::<bucket>)
	// to match the S3 scanner's published bucket resource_id.
	RelationshipAppStreamStackUsesS3Bucket = "appstream_stack_uses_s3_bucket"
)
