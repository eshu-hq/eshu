// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceFSx identifies the regional Amazon FSx metadata-only scan slice.
	// One scanner covers every FSx flavor (Windows File Server, Lustre, NetApp
	// ONTAP, and OpenZFS) for file systems, backups, snapshots, storage virtual
	// machines, and volumes.
	ServiceFSx = "fsx"
)

const (
	// ResourceTypeFSxFileSystem identifies an FSx file system metadata resource.
	// The scanner records the file system type (WINDOWS, LUSTRE, ONTAP,
	// OPENZFS), deployment type, storage and throughput capacity, storage type,
	// and lifecycle. It never persists Active Directory self-managed credentials
	// or the fsxadmin password.
	ResourceTypeFSxFileSystem = "aws_fsx_file_system"
	// ResourceTypeFSxBackup identifies an FSx backup metadata resource (type,
	// lifecycle, size, source file system, and KMS key). Backup contents are
	// never read.
	ResourceTypeFSxBackup = "aws_fsx_backup"
	// ResourceTypeFSxSnapshot identifies an FSx snapshot metadata resource
	// (ONTAP and OpenZFS volume snapshots). Snapshot data is never read.
	ResourceTypeFSxSnapshot = "aws_fsx_snapshot"
	// ResourceTypeFSxStorageVirtualMachine identifies an FSx for NetApp ONTAP
	// storage virtual machine metadata resource. The SVM admin password is never
	// persisted.
	ResourceTypeFSxStorageVirtualMachine = "aws_fsx_storage_virtual_machine"
	// ResourceTypeFSxVolume identifies an FSx volume metadata resource (NetApp
	// ONTAP and OpenZFS volumes). Volume file contents are never read.
	ResourceTypeFSxVolume = "aws_fsx_volume"
)

const (
	// RelationshipFSxFileSystemInVPC records an FSx file system's reported VPC
	// placement.
	RelationshipFSxFileSystemInVPC = "fsx_file_system_in_vpc"
	// RelationshipFSxFileSystemInSubnet records an FSx file system's reported
	// subnet placement (one edge per subnet for Multi-AZ deployments).
	RelationshipFSxFileSystemInSubnet = "fsx_file_system_in_subnet"
	// RelationshipFSxFileSystemUsesKMSKey records an FSx file system's reported
	// at-rest encryption KMS key dependency.
	RelationshipFSxFileSystemUsesKMSKey = "fsx_file_system_uses_kms_key"
	// RelationshipFSxFileSystemUsesADDirectory records an FSx file system's
	// reported AWS Managed Microsoft AD directory join (Windows File Server and,
	// when ONTAP joins at the file-system level, ONTAP). The target is the bare
	// directory ID so it joins a future Directory Service scanner resource.
	RelationshipFSxFileSystemUsesADDirectory = "fsx_file_system_uses_ad_directory"
	// RelationshipFSxBackupTargetsFileSystem records the source file system an
	// FSx backup was taken from.
	RelationshipFSxBackupTargetsFileSystem = "fsx_backup_targets_file_system"
	// RelationshipFSxSVMTargetsFileSystem records the file system an FSx for
	// NetApp ONTAP storage virtual machine belongs to.
	RelationshipFSxSVMTargetsFileSystem = "fsx_svm_targets_file_system"
	// RelationshipFSxSVMUsesADDirectory records an FSx for NetApp ONTAP storage
	// virtual machine's reported AWS Managed Microsoft AD directory join.
	RelationshipFSxSVMUsesADDirectory = "fsx_svm_uses_ad_directory"
	// RelationshipFSxVolumeTargetsSVM records the storage virtual machine an FSx
	// for NetApp ONTAP volume belongs to.
	RelationshipFSxVolumeTargetsSVM = "fsx_volume_targets_svm"
	// RelationshipFSxVolumeTargetsFileSystem records the file system an FSx
	// volume belongs to (ONTAP and OpenZFS).
	RelationshipFSxVolumeTargetsFileSystem = "fsx_volume_targets_file_system"
)
