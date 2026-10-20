// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Storage-category constants consolidated from constants_s3.go (#6696).

const (
	// ServiceS3 identifies the regional Amazon Simple Storage Service bucket
	// metadata scan slice.
	ServiceS3 = "s3"
)

const (
	// ResourceTypeS3Bucket identifies an S3 bucket metadata resource.
	ResourceTypeS3Bucket = "aws_s3_bucket"
)

const (
	// RelationshipS3BucketLogsToBucket records S3 server-access-log delivery
	// metadata from a source bucket to its target bucket.
	RelationshipS3BucketLogsToBucket = "s3_bucket_logs_to_bucket"
)

// Storage-category constants consolidated from constants_efs.go (#6696).

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

// Storage-category constants consolidated from constants_fsx.go (#6696).

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

// Storage-category constants consolidated from constants_storagegateway.go (#6696).

const (
	// ServiceStorageGateway identifies the regional AWS Storage Gateway
	// metadata-only scan slice covering gateways, cached/stored volumes, and
	// NFS/SMB S3 file shares.
	ServiceStorageGateway = "storagegateway"
)

const (
	// ResourceTypeStorageGatewayGateway identifies an AWS Storage Gateway
	// gateway metadata resource. The scanner records gateway identity, type,
	// state, and endpoint type but never activates, deletes, or refreshes a
	// gateway.
	ResourceTypeStorageGatewayGateway = "aws_storagegateway_gateway"
	// ResourceTypeStorageGatewayVolume identifies an AWS Storage Gateway cached
	// or stored iSCSI volume metadata resource.
	ResourceTypeStorageGatewayVolume = "aws_storagegateway_volume"
	// ResourceTypeStorageGatewayFileShare identifies an AWS Storage Gateway
	// NFS or SMB S3 file share metadata resource. File-share object contents,
	// client allow lists, and admin/user lists stay outside the contract.
	ResourceTypeStorageGatewayFileShare = "aws_storagegateway_file_share"
)

const (
	// RelationshipStorageGatewayVolumeOnGateway records a Storage Gateway
	// volume's parent gateway when AWS reports the gateway ARN.
	RelationshipStorageGatewayVolumeOnGateway = "storagegateway_volume_on_gateway"
	// RelationshipStorageGatewayFileShareOnGateway records a Storage Gateway
	// file share's parent gateway when AWS reports the gateway ARN.
	RelationshipStorageGatewayFileShareOnGateway = "storagegateway_file_share_on_gateway"
	// RelationshipStorageGatewayFileShareStoresInS3Bucket records the S3 bucket
	// a file share's reported LocationARN resolves to.
	RelationshipStorageGatewayFileShareStoresInS3Bucket = "storagegateway_file_share_stores_in_s3_bucket"
	// RelationshipStorageGatewayFileShareUsesIAMRole records the IAM role a
	// file share assumes to access its S3 storage when AWS reports a role ARN.
	RelationshipStorageGatewayFileShareUsesIAMRole = "storagegateway_file_share_uses_iam_role"
	// RelationshipStorageGatewayFileShareUsesKMSKey records the KMS key a file
	// share uses for server-side encryption when AWS reports a key ARN.
	RelationshipStorageGatewayFileShareUsesKMSKey = "storagegateway_file_share_uses_kms_key"
	// RelationshipStorageGatewayFileShareLogsToCloudWatch records the
	// CloudWatch Logs log group a file share delivers audit logs to when AWS
	// reports a log-group ARN.
	RelationshipStorageGatewayFileShareLogsToCloudWatch = "storagegateway_file_share_logs_to_cloudwatch"
	// RelationshipStorageGatewayGatewayUsesVPCEndpoint records the VPC endpoint
	// a gateway is activated against when AWS reports a `vpce-`-shaped endpoint
	// identifier that the VPC scanner publishes as a node.
	RelationshipStorageGatewayGatewayUsesVPCEndpoint = "storagegateway_gateway_uses_vpc_endpoint"
)

// Storage-category constants consolidated from constants_backup.go (#6696).

const (
	// ServiceBackup identifies the regional AWS Backup metadata-only scan slice
	// covering backup vaults, backup plans, backup selections, recovery point
	// metadata, report plans, restore testing plans, and framework metadata.
	// Recovery point contents, backup vault access policy bodies, and framework
	// control input parameter values are intentionally outside the scan slice.
	ServiceBackup = "backup"
)

const (
	// ResourceTypeBackupVault identifies an AWS Backup vault metadata resource.
	// The scanner emits identity, encryption, lock state, and recovery-point
	// count summaries; access policy JSON bodies are never persisted.
	ResourceTypeBackupVault = "aws_backup_vault"
	// ResourceTypeBackupPlan identifies an AWS Backup plan metadata resource.
	// The scanner emits identity, version, last-execution date, and rule-level
	// schedule and target-vault metadata only.
	ResourceTypeBackupPlan = "aws_backup_plan"
	// ResourceTypeBackupSelection identifies an AWS Backup selection metadata
	// resource. The scanner emits identity, the IAM role used to back up the
	// selected resources, included resource ARNs, and tag-filter conditions
	// only.
	ResourceTypeBackupSelection = "aws_backup_selection"
	// ResourceTypeBackupRecoveryPoint identifies an AWS Backup recovery point
	// metadata resource. The scanner emits identity, source resource ARN,
	// vault, status, creation/expiration time, and KMS key identity only. It
	// NEVER persists snapshot contents or recovery-point restore metadata
	// values.
	ResourceTypeBackupRecoveryPoint = "aws_backup_recovery_point"
	// ResourceTypeBackupReportPlan identifies an AWS Backup report plan
	// metadata resource. The scanner emits identity, report template, format,
	// and S3 destination metadata only.
	ResourceTypeBackupReportPlan = "aws_backup_report_plan"
	// ResourceTypeBackupRestoreTestingPlan identifies an AWS Backup restore
	// testing plan metadata resource. The scanner emits identity, schedule,
	// timezone, and last-execution timestamp only.
	ResourceTypeBackupRestoreTestingPlan = "aws_backup_restore_testing_plan"
	// ResourceTypeBackupFramework identifies an AWS Backup framework metadata
	// resource. The scanner emits identity, deployment status, and control
	// count summaries only. Framework control input parameter values are
	// never persisted because they may carry compliance-sensitive scope data.
	ResourceTypeBackupFramework = "aws_backup_framework"
	// ResourceTypeBackupFrameworkControl identifies an AWS Backup framework
	// control metadata resource. The scanner emits the control name and
	// control scope identity; control input parameter values are never
	// persisted.
	ResourceTypeBackupFrameworkControl = "aws_backup_framework_control"
)

const (
	// RelationshipBackupPlanHasSelection records membership of an AWS Backup
	// selection inside its owning backup plan.
	RelationshipBackupPlanHasSelection = "backup_plan_has_selection"
	// RelationshipBackupSelectionIncludesResource records a resource ARN that
	// an AWS Backup selection explicitly assigns to its plan.
	RelationshipBackupSelectionIncludesResource = "backup_selection_includes_resource"
	// RelationshipBackupSelectionUsesIAMRole records the IAM role an AWS Backup
	// selection uses to back up the selected resources.
	RelationshipBackupSelectionUsesIAMRole = "backup_selection_uses_iam_role"
	// RelationshipBackupVaultUsesKMSKey records the KMS key reported by an AWS
	// Backup vault's server-side encryption configuration.
	RelationshipBackupVaultUsesKMSKey = "backup_vault_uses_kms_key"
	// RelationshipBackupRecoveryPointInVault records that a recovery point is
	// stored inside its owning backup vault.
	RelationshipBackupRecoveryPointInVault = "backup_recovery_point_in_vault"
	// RelationshipBackupRecoveryPointOfResource records the source resource a
	// recovery point captured, as reported by AWS Backup metadata only.
	RelationshipBackupRecoveryPointOfResource = "backup_recovery_point_of_resource"
	// RelationshipBackupFrameworkHasControl records membership of an AWS
	// Backup framework control inside its owning framework. Control input
	// parameter values are never persisted on either side of the edge.
	RelationshipBackupFrameworkHasControl = "backup_framework_has_control"
)
