// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceWorkSpaces identifies the regional Amazon WorkSpaces metadata-only
	// scan slice. The scanner reads control-plane describe APIs
	// (DescribeWorkspaces, DescribeWorkspaceDirectories, DescribeWorkspaceBundles,
	// DescribeIpGroups, DescribeTags) and never reads desktop session contents,
	// user credentials, registration codes, or connection state, and never
	// creates, modifies, reboots, rebuilds, starts, stops, or terminates a
	// WorkSpace or any WorkSpaces resource.
	ServiceWorkSpaces = "workspaces"
)

const (
	// ResourceTypeWorkSpacesWorkspace identifies an Amazon WorkSpaces virtual
	// desktop metadata resource. The scanner emits identity, the parent
	// directory and bundle identifiers, the operational state, the volume
	// encryption configuration, and the (identity-metadata) user name only.
	// Desktop session contents and connection state stay outside the contract.
	ResourceTypeWorkSpacesWorkspace = "aws_workspaces"
	// ResourceTypeWorkSpacesDirectory identifies an Amazon WorkSpaces registered
	// directory metadata resource. This is the WorkSpaces-side registration of a
	// Directory Service directory; the scanner emits identity, registration
	// state, directory type, tenancy, and the network placement references
	// (subnets, security group, IAM role, IP access control groups). The
	// registration code is intentionally excluded.
	ResourceTypeWorkSpacesDirectory = "aws_workspaces_directory"
	// ResourceTypeWorkSpacesBundle identifies an Amazon WorkSpaces bundle
	// metadata resource. The scanner emits identity, the owner, the bundle type,
	// the compute type, the root/user volume sizes, and the backing image
	// identifier only.
	ResourceTypeWorkSpacesBundle = "aws_workspaces_bundle"
	// ResourceTypeWorkSpacesIPGroup identifies an Amazon WorkSpaces IP access
	// control group metadata resource. The scanner emits identity, the group
	// description, and the CIDR access rules (network configuration, not secret
	// material) only.
	ResourceTypeWorkSpacesIPGroup = "aws_workspaces_ip_group"
)

const (
	// RelationshipWorkSpacesWorkspaceInDirectory records a WorkSpace's membership
	// in its parent WorkSpaces directory. The target is keyed by the directory
	// node's published resource_id so the edge joins the directory node exactly.
	RelationshipWorkSpacesWorkspaceInDirectory = "workspaces_workspace_in_directory"
	// RelationshipWorkSpacesWorkspaceUsesBundle records a WorkSpace's dependency
	// on the bundle it was created from. The target is the WorkSpaces bundle
	// node keyed by the bundle node's published resource_id.
	RelationshipWorkSpacesWorkspaceUsesBundle = "workspaces_workspace_uses_bundle"
	// RelationshipWorkSpacesWorkspaceUsesKMSKey records a WorkSpace's reported
	// volume encryption KMS key dependency.
	RelationshipWorkSpacesWorkspaceUsesKMSKey = "workspaces_workspace_uses_kms_key"
	// RelationshipWorkSpacesDirectoryUsesDSDirectory records the WorkSpaces
	// directory's link to the underlying AWS Directory Service directory. The
	// target is the bare Directory Service directory id (for example
	// "d-1234567890") the ds scanner publishes as its resource_id.
	RelationshipWorkSpacesDirectoryUsesDSDirectory = "workspaces_directory_uses_ds_directory"
	// RelationshipWorkSpacesDirectoryInSubnet records a WorkSpaces directory's
	// placement in a VPC subnet. The target is the bare subnet id the ec2
	// scanner publishes.
	RelationshipWorkSpacesDirectoryInSubnet = "workspaces_directory_in_subnet"
	// RelationshipWorkSpacesDirectoryUsesSecurityGroup records the WorkSpaces
	// security group assigned to new WorkSpaces in the directory. The target is
	// the bare security group id the ec2 scanner publishes.
	RelationshipWorkSpacesDirectoryUsesSecurityGroup = "workspaces_directory_uses_security_group"
	// RelationshipWorkSpacesDirectoryUsesIAMRole records the IAM role WorkSpaces
	// assumes to call other services on the account's behalf. The target is the
	// IAM role ARN the iam scanner publishes as its role resource_id.
	RelationshipWorkSpacesDirectoryUsesIAMRole = "workspaces_directory_uses_iam_role"
	// RelationshipWorkSpacesDirectoryUsesIPGroup records a WorkSpaces directory's
	// association with an IP access control group. The target is the WorkSpaces
	// IP access control group node keyed by its published resource_id.
	RelationshipWorkSpacesDirectoryUsesIPGroup = "workspaces_directory_uses_ip_group"
)
