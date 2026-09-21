// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceEKS identifies the regional Amazon Elastic Kubernetes Service scan
	// slice.
	ServiceEKS = "eks"
)

const (
	// ResourceTypeEKSCluster identifies an EKS cluster.
	ResourceTypeEKSCluster = "aws_eks_cluster"
	// ResourceTypeEKSNodegroup identifies an EKS managed node group.
	ResourceTypeEKSNodegroup = "aws_eks_nodegroup"
	// ResourceTypeEKSAddon identifies an EKS managed add-on.
	ResourceTypeEKSAddon = "aws_eks_addon"
	// ResourceTypeEKSOIDCProvider identifies OIDC provider evidence associated
	// with an EKS cluster.
	ResourceTypeEKSOIDCProvider = "aws_eks_oidc_provider"
)

const (
	// RelationshipEKSClusterUsesIAMRole records an EKS cluster service role.
	RelationshipEKSClusterUsesIAMRole = "eks_cluster_uses_iam_role"
	// RelationshipEKSClusterUsesSubnet records EKS cluster subnet placement.
	RelationshipEKSClusterUsesSubnet = "eks_cluster_uses_subnet"
	// RelationshipEKSClusterUsesSecurityGroup records EKS cluster security group
	// placement.
	RelationshipEKSClusterUsesSecurityGroup = "eks_cluster_uses_security_group"
	// RelationshipEKSClusterHasOIDCProvider records an EKS cluster's OIDC
	// provider evidence for IRSA trust.
	RelationshipEKSClusterHasOIDCProvider = "eks_cluster_has_oidc_provider"
	// RelationshipEKSClusterHasNodegroup records managed node group membership
	// on an EKS cluster.
	RelationshipEKSClusterHasNodegroup = "eks_cluster_has_nodegroup"
	// RelationshipEKSClusterHasAddon records managed add-on membership on an EKS
	// cluster.
	RelationshipEKSClusterHasAddon = "eks_cluster_has_addon"
	// RelationshipEKSNodegroupUsesIAMRole records the IAM role used by an EKS
	// managed node group.
	RelationshipEKSNodegroupUsesIAMRole = "eks_nodegroup_uses_iam_role"
	// RelationshipEKSNodegroupUsesSubnet records an EKS managed node group
	// subnet.
	RelationshipEKSNodegroupUsesSubnet = "eks_nodegroup_uses_subnet"
	// RelationshipEKSAddonUsesIAMRole records the IAM role used by an EKS
	// managed add-on.
	RelationshipEKSAddonUsesIAMRole = "eks_addon_uses_iam_role"
)
