// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"

	fsxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/fsx"
)

// mapFileSystem translates one SDK FileSystem into scanner-owned metadata. The
// flavor-specific configuration blocks are read only for safe deployment,
// throughput, and AD-directory metadata; the Active Directory self-managed
// credential password, the ONTAP fsxadmin password, DNS server IPs, the AD
// service-account user name, and the domain-join secret ARN are never mapped.
func mapFileSystem(description awsfsxtypes.FileSystem) fsxservice.FileSystem {
	fs := fsxservice.FileSystem{
		ID:                    aws.ToString(description.FileSystemId),
		ARN:                   strings.TrimSpace(aws.ToString(description.ResourceARN)),
		FileSystemType:        string(description.FileSystemType),
		StorageType:           string(description.StorageType),
		Lifecycle:             string(description.Lifecycle),
		OwnerID:               aws.ToString(description.OwnerId),
		VPCID:                 aws.ToString(description.VpcId),
		SubnetIDs:             cloneStrings(description.SubnetIds),
		NetworkType:           string(description.NetworkType),
		StorageCapacityGiB:    aws.ToInt32(description.StorageCapacity),
		KMSKeyID:              strings.TrimSpace(aws.ToString(description.KmsKeyId)),
		DNSName:               strings.TrimSpace(aws.ToString(description.DNSName)),
		FileSystemTypeVersion: strings.TrimSpace(aws.ToString(description.FileSystemTypeVersion)),
		Tags:                  mapTags(description.Tags),
	}
	applyWindowsConfig(&fs, description.WindowsConfiguration)
	applyOntapConfig(&fs, description.OntapConfiguration)
	applyOpenZFSConfig(&fs, description.OpenZFSConfiguration)
	applyLustreConfig(&fs, description.LustreConfiguration)
	return fs
}

// applyWindowsConfig reads Windows deployment, throughput, preferred subnet,
// and the AWS Managed Microsoft AD directory ID. The self-managed AD
// attributes (including FileSystemAdministratorsGroup and UserName) are never
// read; the describe-time type carries no password.
func applyWindowsConfig(fs *fsxservice.FileSystem, config *awsfsxtypes.WindowsFileSystemConfiguration) {
	if config == nil {
		return
	}
	fs.DeploymentType = string(config.DeploymentType)
	fs.ThroughputCapacityMBps = config.ThroughputCapacity
	fs.PreferredSubnetID = strings.TrimSpace(aws.ToString(config.PreferredSubnetId))
	fs.ActiveDirectoryID = strings.TrimSpace(aws.ToString(config.ActiveDirectoryId))
}

// applyOntapConfig reads ONTAP deployment, throughput, and preferred subnet.
// FsxAdminPassword is never read.
func applyOntapConfig(fs *fsxservice.FileSystem, config *awsfsxtypes.OntapFileSystemConfiguration) {
	if config == nil {
		return
	}
	fs.DeploymentType = string(config.DeploymentType)
	fs.ThroughputCapacityMBps = config.ThroughputCapacity
	fs.PreferredSubnetID = strings.TrimSpace(aws.ToString(config.PreferredSubnetId))
}

// applyOpenZFSConfig reads OpenZFS deployment, throughput, and preferred subnet.
func applyOpenZFSConfig(fs *fsxservice.FileSystem, config *awsfsxtypes.OpenZFSFileSystemConfiguration) {
	if config == nil {
		return
	}
	fs.DeploymentType = string(config.DeploymentType)
	fs.ThroughputCapacityMBps = config.ThroughputCapacity
	fs.PreferredSubnetID = strings.TrimSpace(aws.ToString(config.PreferredSubnetId))
}

// applyLustreConfig reads Lustre deployment type and per-unit storage
// throughput.
func applyLustreConfig(fs *fsxservice.FileSystem, config *awsfsxtypes.LustreFileSystemConfiguration) {
	if config == nil {
		return
	}
	fs.DeploymentType = string(config.DeploymentType)
	fs.PerUnitStorageThroughput = config.PerUnitStorageThroughput
}

// mapStorageVirtualMachine translates one SDK StorageVirtualMachine into
// scanner-owned metadata. Only the NetBIOS name and the AWS Managed Microsoft
// AD directory ID are read from the AD configuration; the self-managed AD
// service-account credentials and the SVM admin password are never mapped.
func mapStorageVirtualMachine(description awsfsxtypes.StorageVirtualMachine) fsxservice.StorageVirtualMachine {
	svm := fsxservice.StorageVirtualMachine{
		ID:           aws.ToString(description.StorageVirtualMachineId),
		ARN:          strings.TrimSpace(aws.ToString(description.ResourceARN)),
		Name:         strings.TrimSpace(aws.ToString(description.Name)),
		FileSystemID: aws.ToString(description.FileSystemId),
		Lifecycle:    string(description.Lifecycle),
		Subtype:      string(description.Subtype),
		UUID:         strings.TrimSpace(aws.ToString(description.UUID)),
		Tags:         mapTags(description.Tags),
	}
	if ad := description.ActiveDirectoryConfiguration; ad != nil {
		svm.NetBiosName = strings.TrimSpace(aws.ToString(ad.NetBiosName))
		// The describe-time SelfManagedActiveDirectoryAttributes carries no
		// password and no AWS Managed Microsoft AD directory ID, so nothing
		// from it is mapped here. Windows/ONTAP managed-AD joins surface the
		// directory ID at the file-system level instead.
	}
	return svm
}

// mapVolume translates one SDK Volume into scanner-owned metadata for ONTAP and
// OpenZFS volumes. File contents are never read.
func mapVolume(description awsfsxtypes.Volume) fsxservice.Volume {
	volume := fsxservice.Volume{
		ID:           aws.ToString(description.VolumeId),
		ARN:          strings.TrimSpace(aws.ToString(description.ResourceARN)),
		Name:         strings.TrimSpace(aws.ToString(description.Name)),
		FileSystemID: aws.ToString(description.FileSystemId),
		VolumeType:   string(description.VolumeType),
		Lifecycle:    string(description.Lifecycle),
		Tags:         mapTags(description.Tags),
	}
	if ontap := description.OntapConfiguration; ontap != nil {
		volume.StorageVirtualMachineID = aws.ToString(ontap.StorageVirtualMachineId)
		volume.JunctionPath = strings.TrimSpace(aws.ToString(ontap.JunctionPath))
		volume.SizeInMegabytes = ontap.SizeInMegabytes
	}
	if zfs := description.OpenZFSConfiguration; zfs != nil {
		volume.VolumePath = strings.TrimSpace(aws.ToString(zfs.VolumePath))
		volume.StorageCapacityQuotaGiB = zfs.StorageCapacityQuotaGiB
	}
	return volume
}

// mapSnapshot translates one SDK Snapshot into scanner-owned metadata.
func mapSnapshot(description awsfsxtypes.Snapshot) fsxservice.Snapshot {
	return fsxservice.Snapshot{
		ID:        aws.ToString(description.SnapshotId),
		ARN:       strings.TrimSpace(aws.ToString(description.ResourceARN)),
		Name:      strings.TrimSpace(aws.ToString(description.Name)),
		VolumeID:  aws.ToString(description.VolumeId),
		Lifecycle: string(description.Lifecycle),
		Tags:      mapTags(description.Tags),
	}
}

// mapBackup translates one SDK Backup into scanner-owned metadata. The source
// file system identity is read from the embedded FileSystem record for the
// backup-to-file-system relationship. Backup contents are never read.
func mapBackup(description awsfsxtypes.Backup) fsxservice.Backup {
	backup := fsxservice.Backup{
		ID:             aws.ToString(description.BackupId),
		ARN:            strings.TrimSpace(aws.ToString(description.ResourceARN)),
		Type:           string(description.Type),
		Lifecycle:      string(description.Lifecycle),
		OwnerID:        aws.ToString(description.OwnerId),
		KMSKeyID:       strings.TrimSpace(aws.ToString(description.KmsKeyId)),
		SizeInBytes:    description.SizeInBytes,
		ResourceType:   string(description.ResourceType),
		SourceBackupID: strings.TrimSpace(aws.ToString(description.SourceBackupId)),
		Tags:           mapTags(description.Tags),
	}
	if fs := description.FileSystem; fs != nil {
		backup.FileSystemID = aws.ToString(fs.FileSystemId)
		backup.FileSystemARN = strings.TrimSpace(aws.ToString(fs.ResourceARN))
	}
	if volume := description.Volume; volume != nil {
		backup.VolumeID = aws.ToString(volume.VolumeId)
	}
	return backup
}

func mapTags(tags []awsfsxtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
