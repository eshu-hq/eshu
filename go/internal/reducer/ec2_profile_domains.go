// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

const (
	// DomainEC2UsesProfileMaterialization projects ec2_instance_posture
	// instance_profile_arn into canonical USES_PROFILE edges between an EC2
	// instance CloudResource node and the IAM instance-profile CloudResource node
	// it uses. It is EDGE-ONLY on the existing cloud_resource_uid keyspace (no new
	// node type): the source EC2 instance node is materialized by
	// DomainEC2InstanceNodeMaterialization (#1146 PR-A) and the target
	// instance-profile node by DomainAWSResourceMaterialization (#805). Because the
	// two endpoint nodes publish their canonical_nodes_committed phase under
	// different entity keys, the edge gates on both phases so it never resolves
	// against an endpoint that has not committed.
	DomainEC2UsesProfileMaterialization Domain = "ec2_uses_profile_materialization"

	// DomainIAMInstanceProfileRoleMaterialization projects IAM instance-profile
	// aws_resource role_arns into canonical HAS_ROLE edges between the instance
	// profile CloudResource node and each attached IAM role CloudResource node. It
	// is EDGE-ONLY on the existing cloud_resource_uid keyspace; both endpoint node
	// types are materialized by DomainAWSResourceMaterialization (#805). Profiles
	// with no roles produce no edge and are not counted as skips; unscanned roles
	// are counted and never fabricated.
	DomainIAMInstanceProfileRoleMaterialization Domain = "iam_instance_profile_role_materialization"
)
