// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceEFS identifies the regional Amazon Elastic File System metadata-only
	// scan slice.
	ServiceEFS = "efs"
)

const (
	// ResourceTypeEFSFileSystem identifies an EFS file system metadata resource.
	// The scanner records performance mode, throughput mode, encryption status,
	// and a lifecycle policy summary; NFS file system policy bodies are never
	// persisted.
	ResourceTypeEFSFileSystem = "aws_efs_file_system"
	// ResourceTypeEFSAccessPoint identifies an EFS access point metadata
	// resource.
	ResourceTypeEFSAccessPoint = "aws_efs_access_point"
	// ResourceTypeEFSMountTarget identifies an EFS mount target metadata
	// resource.
	ResourceTypeEFSMountTarget = "aws_efs_mount_target"
	// ResourceTypeEFSReplicationConfiguration identifies an EFS replication
	// configuration metadata resource keyed by its source file system.
	ResourceTypeEFSReplicationConfiguration = "aws_efs_replication_configuration"
)

const (
	// RelationshipEFSMountTargetInSubnet records an EFS mount target's reported
	// subnet placement.
	RelationshipEFSMountTargetInSubnet = "efs_mount_target_in_subnet"
	// RelationshipEFSMountTargetUsesSecurityGroup records an EFS mount target's
	// reported security group attachment.
	RelationshipEFSMountTargetUsesSecurityGroup = "efs_mount_target_uses_security_group"
	// RelationshipEFSFileSystemUsesKMSKey records an EFS file system's reported
	// encryption KMS key dependency.
	RelationshipEFSFileSystemUsesKMSKey = "efs_file_system_uses_kms_key"
	// RelationshipEFSAccessPointTargetsFileSystem records the file system an EFS
	// access point exposes.
	RelationshipEFSAccessPointTargetsFileSystem = "efs_access_point_targets_file_system"
	// RelationshipEFSReplicationTargetsFileSystem records the destination file
	// system an EFS replication configuration writes to.
	RelationshipEFSReplicationTargetsFileSystem = "efs_replication_targets_file_system"
)
