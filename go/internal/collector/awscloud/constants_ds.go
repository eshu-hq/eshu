// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDirectoryService identifies the regional AWS Directory Service
	// metadata-only scan slice. One scanner covers AWS Managed Microsoft AD,
	// Simple AD, and AD Connector directories plus their trust relationships,
	// shared-directory invitations, and LDAPS settings metadata.
	ServiceDirectoryService = "ds"
)

const (
	// ResourceTypeDSDirectory identifies an AWS Directory Service directory
	// metadata resource (type MicrosoftAD/SimpleAD/ADConnector, edition, size,
	// stage, and VPC settings). The scanner records the bare directory ID
	// (d-xxxxxxxxxx) as the resource_id so FSx Active Directory join edges, which
	// target the bare directory ID, resolve against it. Directory admin
	// passwords and RADIUS shared secrets are never read.
	ResourceTypeDSDirectory = "aws_ds_directory"
	// ResourceTypeDSTrust identifies an AWS Directory Service trust relationship
	// metadata resource (trust id, direction, type, state, and the FQDN of the
	// external domain). The scanner records the trust id (t-xxxxxxxxxx) as the
	// resource_id.
	ResourceTypeDSTrust = "aws_ds_trust"
	// ResourceTypeDSSharedDirectory identifies an AWS Directory Service shared
	// directory metadata resource (owner account, owner directory, shared
	// account, shared directory, share method, and share status). The scanner
	// records the owner directory id paired with the shared account id as the
	// resource_id so each share invitation is distinct.
	ResourceTypeDSSharedDirectory = "aws_ds_shared_directory"
)

const (
	// RelationshipDSDirectoryInVPC records a directory's reported VPC placement.
	// The target is the bare vpc-id so it joins the VPC scanner's resource_id.
	RelationshipDSDirectoryInVPC = "ds_directory_in_vpc"
	// RelationshipDSDirectoryInSubnet records a directory's reported subnet
	// placement (one edge per subnet). The target is the bare subnet-id so it
	// joins the VPC scanner's subnet resource_id.
	RelationshipDSDirectoryInSubnet = "ds_directory_in_subnet"
	// RelationshipDSTrustTargetsDirectory records the local directory a trust
	// relationship belongs to. The target is the bare directory id so it joins
	// the directory resource fact emitted in the same scan.
	RelationshipDSTrustTargetsDirectory = "ds_trust_targets_directory"
	// RelationshipDSSharedDirectoryTargetsOwnerDirectory records the owner
	// directory a shared-directory invitation references. The target is the bare
	// owner directory id so it joins the directory resource fact when the owner
	// directory is in scope.
	RelationshipDSSharedDirectoryTargetsOwnerDirectory = "ds_shared_directory_targets_owner_directory"
	// RelationshipDSSharedDirectoryTargetsOwnerAccount records the AWS account
	// that owns the shared directory. The target is the bare 12-digit account id
	// so it joins an account resource by id; target_arn is never synthesized.
	RelationshipDSSharedDirectoryTargetsOwnerAccount = "ds_shared_directory_targets_owner_account"
)
