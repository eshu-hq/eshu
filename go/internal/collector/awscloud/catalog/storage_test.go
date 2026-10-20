// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestStorageConstantsMatchAWSCloudValues pins every storage-category constant
// to the exact value it carried in package awscloud before the #6696 catalog
// consolidation. The consolidation moves declarations only: any value drift
// here is a behavior change, not a move.
func TestStorageConstantsMatchAWSCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "RelationshipBackupFrameworkHasControl", got: RelationshipBackupFrameworkHasControl, want: "backup_framework_has_control"},
		{name: "RelationshipBackupPlanHasSelection", got: RelationshipBackupPlanHasSelection, want: "backup_plan_has_selection"},
		{name: "RelationshipBackupRecoveryPointInVault", got: RelationshipBackupRecoveryPointInVault, want: "backup_recovery_point_in_vault"},
		{name: "RelationshipBackupRecoveryPointOfResource", got: RelationshipBackupRecoveryPointOfResource, want: "backup_recovery_point_of_resource"},
		{name: "RelationshipBackupSelectionIncludesResource", got: RelationshipBackupSelectionIncludesResource, want: "backup_selection_includes_resource"},
		{name: "RelationshipBackupSelectionUsesIAMRole", got: RelationshipBackupSelectionUsesIAMRole, want: "backup_selection_uses_iam_role"},
		{name: "RelationshipBackupVaultUsesKMSKey", got: RelationshipBackupVaultUsesKMSKey, want: "backup_vault_uses_kms_key"},
		{name: "RelationshipEFSAccessPointTargetsFileSystem", got: RelationshipEFSAccessPointTargetsFileSystem, want: "efs_access_point_targets_file_system"},
		{name: "RelationshipEFSFileSystemUsesKMSKey", got: RelationshipEFSFileSystemUsesKMSKey, want: "efs_file_system_uses_kms_key"},
		{name: "RelationshipEFSMountTargetInSubnet", got: RelationshipEFSMountTargetInSubnet, want: "efs_mount_target_in_subnet"},
		{name: "RelationshipEFSMountTargetUsesSecurityGroup", got: RelationshipEFSMountTargetUsesSecurityGroup, want: "efs_mount_target_uses_security_group"},
		{name: "RelationshipEFSReplicationTargetsFileSystem", got: RelationshipEFSReplicationTargetsFileSystem, want: "efs_replication_targets_file_system"},
		{name: "RelationshipFSxBackupTargetsFileSystem", got: RelationshipFSxBackupTargetsFileSystem, want: "fsx_backup_targets_file_system"},
		{name: "RelationshipFSxFileSystemInSubnet", got: RelationshipFSxFileSystemInSubnet, want: "fsx_file_system_in_subnet"},
		{name: "RelationshipFSxFileSystemInVPC", got: RelationshipFSxFileSystemInVPC, want: "fsx_file_system_in_vpc"},
		{name: "RelationshipFSxFileSystemUsesADDirectory", got: RelationshipFSxFileSystemUsesADDirectory, want: "fsx_file_system_uses_ad_directory"},
		{name: "RelationshipFSxFileSystemUsesKMSKey", got: RelationshipFSxFileSystemUsesKMSKey, want: "fsx_file_system_uses_kms_key"},
		{name: "RelationshipFSxSVMTargetsFileSystem", got: RelationshipFSxSVMTargetsFileSystem, want: "fsx_svm_targets_file_system"},
		{name: "RelationshipFSxSVMUsesADDirectory", got: RelationshipFSxSVMUsesADDirectory, want: "fsx_svm_uses_ad_directory"},
		{name: "RelationshipFSxVolumeTargetsFileSystem", got: RelationshipFSxVolumeTargetsFileSystem, want: "fsx_volume_targets_file_system"},
		{name: "RelationshipFSxVolumeTargetsSVM", got: RelationshipFSxVolumeTargetsSVM, want: "fsx_volume_targets_svm"},
		{name: "RelationshipS3BucketLogsToBucket", got: RelationshipS3BucketLogsToBucket, want: "s3_bucket_logs_to_bucket"},
		{name: "RelationshipStorageGatewayFileShareLogsToCloudWatch", got: RelationshipStorageGatewayFileShareLogsToCloudWatch, want: "storagegateway_file_share_logs_to_cloudwatch"},
		{name: "RelationshipStorageGatewayFileShareOnGateway", got: RelationshipStorageGatewayFileShareOnGateway, want: "storagegateway_file_share_on_gateway"},
		{name: "RelationshipStorageGatewayFileShareStoresInS3Bucket", got: RelationshipStorageGatewayFileShareStoresInS3Bucket, want: "storagegateway_file_share_stores_in_s3_bucket"},
		{name: "RelationshipStorageGatewayFileShareUsesIAMRole", got: RelationshipStorageGatewayFileShareUsesIAMRole, want: "storagegateway_file_share_uses_iam_role"},
		{name: "RelationshipStorageGatewayFileShareUsesKMSKey", got: RelationshipStorageGatewayFileShareUsesKMSKey, want: "storagegateway_file_share_uses_kms_key"},
		{name: "RelationshipStorageGatewayGatewayUsesVPCEndpoint", got: RelationshipStorageGatewayGatewayUsesVPCEndpoint, want: "storagegateway_gateway_uses_vpc_endpoint"},
		{name: "RelationshipStorageGatewayVolumeOnGateway", got: RelationshipStorageGatewayVolumeOnGateway, want: "storagegateway_volume_on_gateway"},
		{name: "ResourceTypeBackupFramework", got: ResourceTypeBackupFramework, want: "aws_backup_framework"},
		{name: "ResourceTypeBackupFrameworkControl", got: ResourceTypeBackupFrameworkControl, want: "aws_backup_framework_control"},
		{name: "ResourceTypeBackupPlan", got: ResourceTypeBackupPlan, want: "aws_backup_plan"},
		{name: "ResourceTypeBackupRecoveryPoint", got: ResourceTypeBackupRecoveryPoint, want: "aws_backup_recovery_point"},
		{name: "ResourceTypeBackupReportPlan", got: ResourceTypeBackupReportPlan, want: "aws_backup_report_plan"},
		{name: "ResourceTypeBackupRestoreTestingPlan", got: ResourceTypeBackupRestoreTestingPlan, want: "aws_backup_restore_testing_plan"},
		{name: "ResourceTypeBackupSelection", got: ResourceTypeBackupSelection, want: "aws_backup_selection"},
		{name: "ResourceTypeBackupVault", got: ResourceTypeBackupVault, want: "aws_backup_vault"},
		{name: "ResourceTypeEFSAccessPoint", got: ResourceTypeEFSAccessPoint, want: "aws_efs_access_point"},
		{name: "ResourceTypeEFSFileSystem", got: ResourceTypeEFSFileSystem, want: "aws_efs_file_system"},
		{name: "ResourceTypeEFSMountTarget", got: ResourceTypeEFSMountTarget, want: "aws_efs_mount_target"},
		{name: "ResourceTypeEFSReplicationConfiguration", got: ResourceTypeEFSReplicationConfiguration, want: "aws_efs_replication_configuration"},
		{name: "ResourceTypeFSxBackup", got: ResourceTypeFSxBackup, want: "aws_fsx_backup"},
		{name: "ResourceTypeFSxFileSystem", got: ResourceTypeFSxFileSystem, want: "aws_fsx_file_system"},
		{name: "ResourceTypeFSxSnapshot", got: ResourceTypeFSxSnapshot, want: "aws_fsx_snapshot"},
		{name: "ResourceTypeFSxStorageVirtualMachine", got: ResourceTypeFSxStorageVirtualMachine, want: "aws_fsx_storage_virtual_machine"},
		{name: "ResourceTypeFSxVolume", got: ResourceTypeFSxVolume, want: "aws_fsx_volume"},
		{name: "ResourceTypeS3Bucket", got: ResourceTypeS3Bucket, want: "aws_s3_bucket"},
		{name: "ResourceTypeStorageGatewayFileShare", got: ResourceTypeStorageGatewayFileShare, want: "aws_storagegateway_file_share"},
		{name: "ResourceTypeStorageGatewayGateway", got: ResourceTypeStorageGatewayGateway, want: "aws_storagegateway_gateway"},
		{name: "ResourceTypeStorageGatewayVolume", got: ResourceTypeStorageGatewayVolume, want: "aws_storagegateway_volume"},
		{name: "ServiceBackup", got: ServiceBackup, want: "backup"},
		{name: "ServiceEFS", got: ServiceEFS, want: "efs"},
		{name: "ServiceFSx", got: ServiceFSx, want: "fsx"},
		{name: "ServiceS3", got: ServiceS3, want: "s3"},
		{name: "ServiceStorageGateway", got: ServiceStorageGateway, want: "storagegateway"},
	}
	for _, tt := range tests {
		tt, name := tt, tt.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q (byte-identical to pre-consolidation awscloud value)", tt.name, tt.got, tt.want)
			}
		})
	}
}
