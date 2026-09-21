// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfsx "github.com/aws/aws-sdk-go-v2/service/fsx"
	awsfsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutationAndContentAPIs is the primary contract
// guard for issue #735. apiClient is the single seam between the FSx adapter
// and the AWS SDK client (Client.client is typed as apiClient, pinned by
// var _ apiClient = (*awsfsx.Client)(nil) in client.go), so any SDK method the
// adapter could call must be listed here. A regression that added a mutation
// API (Create/Delete/Update/Restore/Copy/Release) or a file-content read would
// either fail to compile against this interface or trip this shape assertion.
func TestAPIClientInterfaceExcludesMutationAndContentAPIs(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	want := map[string]bool{
		"DescribeFileSystems":            true,
		"DescribeBackups":                true,
		"DescribeStorageVirtualMachines": true,
		"DescribeVolumes":                true,
		"DescribeSnapshots":              true,
	}
	have := map[string]bool{}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		have[ifaceType.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("apiClient missing required metadata-read method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("apiClient exposes unexpected method %q; metadata-only contract violated", name)
		}
	}

	// Defensive check: no method on the SDK seam may name a forbidden mutation
	// API across any flavor. Mirrors the issue #735 forbidden-API language.
	forbiddenSubstrings := []string{
		"Create", "Update", "Delete", "Put", "Modify", "Restore", "Copy",
		"Release", "Tag", "Untag", "Associate", "Disassociate", "Start",
		"Cancel",
	}
	for name := range have {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("apiClient method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}

func TestClientListFileSystemsMapsEveryFlavor(t *testing.T) {
	tput := int32(512)
	perUnit := int32(125)
	fake := &fakeFSxAPI{
		fileSystems: []awsfsxtypes.FileSystem{
			{
				FileSystemId:    aws.String("fs-windows01"),
				ResourceARN:     aws.String("arn:aws:fsx:us-east-1:123456789012:file-system/fs-windows01"),
				FileSystemType:  awsfsxtypes.FileSystemTypeWindows,
				StorageType:     awsfsxtypes.StorageTypeSsd,
				Lifecycle:       awsfsxtypes.FileSystemLifecycleAvailable,
				VpcId:           aws.String("vpc-aaa"),
				SubnetIds:       []string{"subnet-1", "subnet-2"},
				StorageCapacity: aws.Int32(2048),
				KmsKeyId:        aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
				DNSName:         aws.String("fs-windows01.example.com"),
				WindowsConfiguration: &awsfsxtypes.WindowsFileSystemConfiguration{
					DeploymentType:     awsfsxtypes.WindowsDeploymentTypeMultiAz1,
					ThroughputCapacity: &tput,
					PreferredSubnetId:  aws.String("subnet-1"),
					ActiveDirectoryId:  aws.String("d-1234567890"),
					// SelfManagedActiveDirectoryConfiguration carries the
					// FileSystemAdministratorsGroup and UserName; the adapter
					// must never read them.
					SelfManagedActiveDirectoryConfiguration: &awsfsxtypes.SelfManagedActiveDirectoryAttributes{
						DomainName:                     aws.String("corp.example.com"),
						FileSystemAdministratorsGroup:  aws.String("Domain Admins"),
						UserName:                       aws.String("svc-fsx"),
						DnsIps:                         []string{"10.0.0.10", "10.0.0.11"},
						DomainJoinServiceAccountSecret: aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:ad-join"),
					},
				},
			},
			{
				FileSystemId:          aws.String("fs-lustre01"),
				ResourceARN:           aws.String("arn:aws:fsx:us-east-1:123456789012:file-system/fs-lustre01"),
				FileSystemType:        awsfsxtypes.FileSystemTypeLustre,
				Lifecycle:             awsfsxtypes.FileSystemLifecycleAvailable,
				VpcId:                 aws.String("vpc-aaa"),
				SubnetIds:             []string{"subnet-1"},
				FileSystemTypeVersion: aws.String("2.15"),
				LustreConfiguration: &awsfsxtypes.LustreFileSystemConfiguration{
					DeploymentType:           awsfsxtypes.LustreDeploymentTypePersistent2,
					PerUnitStorageThroughput: &perUnit,
				},
			},
			{
				FileSystemId:   aws.String("fs-ontap01"),
				ResourceARN:    aws.String("arn:aws:fsx:us-east-1:123456789012:file-system/fs-ontap01"),
				FileSystemType: awsfsxtypes.FileSystemTypeOntap,
				Lifecycle:      awsfsxtypes.FileSystemLifecycleAvailable,
				VpcId:          aws.String("vpc-aaa"),
				SubnetIds:      []string{"subnet-1", "subnet-2"},
				OntapConfiguration: &awsfsxtypes.OntapFileSystemConfiguration{
					DeploymentType:     awsfsxtypes.OntapDeploymentTypeMultiAz1,
					ThroughputCapacity: &tput,
					PreferredSubnetId:  aws.String("subnet-1"),
					// FsxAdminPassword is always redacted by AWS; the adapter
					// must never map it regardless.
					FsxAdminPassword: aws.String("REDACTED-BUT-NEVER-MAP"),
				},
			},
			{
				FileSystemId:   aws.String("fs-zfs01"),
				ResourceARN:    aws.String("arn:aws:fsx:us-east-1:123456789012:file-system/fs-zfs01"),
				FileSystemType: awsfsxtypes.FileSystemTypeOpenzfs,
				Lifecycle:      awsfsxtypes.FileSystemLifecycleAvailable,
				VpcId:          aws.String("vpc-aaa"),
				SubnetIds:      []string{"subnet-1"},
				OpenZFSConfiguration: &awsfsxtypes.OpenZFSFileSystemConfiguration{
					DeploymentType:     awsfsxtypes.OpenZFSDeploymentTypeSingleAz2,
					ThroughputCapacity: &tput,
					PreferredSubnetId:  aws.String("subnet-1"),
				},
			},
		},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}

	systems, err := adapter.ListFileSystems(context.Background())
	if err != nil {
		t.Fatalf("ListFileSystems() error = %v, want nil", err)
	}
	if got, want := len(systems), 4; got != want {
		t.Fatalf("len(systems) = %d, want %d", got, want)
	}
	byID := map[string]int{}
	for i, fs := range systems {
		byID[fs.ID] = i
	}

	win := systems[byID["fs-windows01"]]
	if win.FileSystemType != "WINDOWS" || win.DeploymentType != "MULTI_AZ_1" {
		t.Fatalf("windows mapping = %q/%q", win.FileSystemType, win.DeploymentType)
	}
	if win.ThroughputCapacityMBps == nil || *win.ThroughputCapacityMBps != 512 {
		t.Fatalf("windows throughput = %v, want 512", win.ThroughputCapacityMBps)
	}
	if win.ActiveDirectoryID != "d-1234567890" {
		t.Fatalf("windows ActiveDirectoryID = %q, want d-1234567890", win.ActiveDirectoryID)
	}
	if win.PreferredSubnetID != "subnet-1" {
		t.Fatalf("windows PreferredSubnetID = %q, want subnet-1", win.PreferredSubnetID)
	}

	lustre := systems[byID["fs-lustre01"]]
	if lustre.FileSystemType != "LUSTRE" || lustre.DeploymentType != "PERSISTENT_2" {
		t.Fatalf("lustre mapping = %q/%q", lustre.FileSystemType, lustre.DeploymentType)
	}
	if lustre.PerUnitStorageThroughput == nil || *lustre.PerUnitStorageThroughput != 125 {
		t.Fatalf("lustre per-unit throughput = %v, want 125", lustre.PerUnitStorageThroughput)
	}
	if lustre.FileSystemTypeVersion != "2.15" {
		t.Fatalf("lustre version = %q, want 2.15", lustre.FileSystemTypeVersion)
	}

	ontap := systems[byID["fs-ontap01"]]
	if ontap.FileSystemType != "ONTAP" || ontap.DeploymentType != "MULTI_AZ_1" {
		t.Fatalf("ontap mapping = %q/%q", ontap.FileSystemType, ontap.DeploymentType)
	}

	zfs := systems[byID["fs-zfs01"]]
	if zfs.FileSystemType != "OPENZFS" || zfs.DeploymentType != "SINGLE_AZ_2" {
		t.Fatalf("openzfs mapping = %q/%q", zfs.FileSystemType, zfs.DeploymentType)
	}
}

func TestClientListStorageVirtualMachinesNeverMapsAdminPassword(t *testing.T) {
	fake := &fakeFSxAPI{
		storageVirtualMachines: []awsfsxtypes.StorageVirtualMachine{{
			StorageVirtualMachineId: aws.String("svm-0001"),
			ResourceARN:             aws.String("arn:aws:fsx:us-east-1:123456789012:storage-virtual-machine/svm-0001"),
			Name:                    aws.String("svm1"),
			FileSystemId:            aws.String("fs-ontap01"),
			Lifecycle:               awsfsxtypes.StorageVirtualMachineLifecycleCreated,
			Subtype:                 awsfsxtypes.StorageVirtualMachineSubtypeDefault,
			UUID:                    aws.String("uuid-1"),
			ActiveDirectoryConfiguration: &awsfsxtypes.SvmActiveDirectoryConfiguration{
				NetBiosName: aws.String("SVM1"),
				SelfManagedActiveDirectoryConfiguration: &awsfsxtypes.SelfManagedActiveDirectoryAttributes{
					DomainName: aws.String("corp.example.com"),
					UserName:   aws.String("svc-fsx"),
					DnsIps:     []string{"10.0.0.10"},
				},
			},
		}},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}

	svms, err := adapter.ListStorageVirtualMachines(context.Background())
	if err != nil {
		t.Fatalf("ListStorageVirtualMachines() error = %v, want nil", err)
	}
	if got, want := len(svms), 1; got != want {
		t.Fatalf("len(svms) = %d, want %d", got, want)
	}
	svm := svms[0]
	if svm.ID != "svm-0001" || svm.FileSystemID != "fs-ontap01" {
		t.Fatalf("svm identity = %q/%q", svm.ID, svm.FileSystemID)
	}
	if svm.NetBiosName != "SVM1" {
		t.Fatalf("svm NetBiosName = %q, want SVM1", svm.NetBiosName)
	}
	// The scanner-owned StorageVirtualMachine type has no password or
	// self-managed AD credential field by construction. This test documents the
	// describe path never carries the SVM admin password or the self-managed AD
	// service account into scanner-owned metadata.
}

func TestClientListVolumesMapsOntapAndOpenZFS(t *testing.T) {
	volSize := int32(102400)
	quota := int32(100)
	fake := &fakeFSxAPI{
		volumes: []awsfsxtypes.Volume{
			{
				VolumeId:     aws.String("fsvol-ontap01"),
				ResourceARN:  aws.String("arn:aws:fsx:us-east-1:123456789012:volume/fsvol-ontap01"),
				Name:         aws.String("ontapvol"),
				FileSystemId: aws.String("fs-ontap01"),
				VolumeType:   awsfsxtypes.VolumeTypeOntap,
				Lifecycle:    awsfsxtypes.VolumeLifecycleCreated,
				OntapConfiguration: &awsfsxtypes.OntapVolumeConfiguration{
					StorageVirtualMachineId: aws.String("svm-0001"),
					JunctionPath:            aws.String("/vol1"),
					SizeInMegabytes:         &volSize,
				},
			},
			{
				VolumeId:     aws.String("fsvol-zfs01"),
				ResourceARN:  aws.String("arn:aws:fsx:us-east-1:123456789012:volume/fsvol-zfs01"),
				Name:         aws.String("zfsvol"),
				FileSystemId: aws.String("fs-zfs01"),
				VolumeType:   awsfsxtypes.VolumeTypeOpenzfs,
				Lifecycle:    awsfsxtypes.VolumeLifecycleCreated,
				OpenZFSConfiguration: &awsfsxtypes.OpenZFSVolumeConfiguration{
					VolumePath:              aws.String("/fsx/zfs"),
					StorageCapacityQuotaGiB: &quota,
				},
			},
		},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}

	volumes, err := adapter.ListVolumes(context.Background())
	if err != nil {
		t.Fatalf("ListVolumes() error = %v, want nil", err)
	}
	if got, want := len(volumes), 2; got != want {
		t.Fatalf("len(volumes) = %d, want %d", got, want)
	}
	ontap := volumes[0]
	if ontap.StorageVirtualMachineID != "svm-0001" || ontap.JunctionPath != "/vol1" {
		t.Fatalf("ontap volume mapping = %q/%q", ontap.StorageVirtualMachineID, ontap.JunctionPath)
	}
	if ontap.SizeInMegabytes == nil || *ontap.SizeInMegabytes != 102400 {
		t.Fatalf("ontap volume size = %v, want 102400", ontap.SizeInMegabytes)
	}
	zfs := volumes[1]
	if zfs.VolumePath != "/fsx/zfs" {
		t.Fatalf("openzfs VolumePath = %q, want /fsx/zfs", zfs.VolumePath)
	}
}

func TestClientListBackupsMapsSourceFileSystem(t *testing.T) {
	size := int64(4096)
	fake := &fakeFSxAPI{
		backups: []awsfsxtypes.Backup{{
			BackupId:     aws.String("backup-0001"),
			ResourceARN:  aws.String("arn:aws:fsx:us-east-1:123456789012:backup/backup-0001"),
			Type:         awsfsxtypes.BackupTypeAutomatic,
			Lifecycle:    awsfsxtypes.BackupLifecycleAvailable,
			KmsKeyId:     aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
			SizeInBytes:  &size,
			ResourceType: awsfsxtypes.ResourceTypeFileSystem,
			FileSystem: &awsfsxtypes.FileSystem{
				FileSystemId: aws.String("fs-windows01"),
				ResourceARN:  aws.String("arn:aws:fsx:us-east-1:123456789012:file-system/fs-windows01"),
			},
		}},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}

	backups, err := adapter.ListBackups(context.Background())
	if err != nil {
		t.Fatalf("ListBackups() error = %v, want nil", err)
	}
	if got, want := len(backups), 1; got != want {
		t.Fatalf("len(backups) = %d, want %d", got, want)
	}
	backup := backups[0]
	if backup.FileSystemID != "fs-windows01" {
		t.Fatalf("backup FileSystemID = %q, want fs-windows01", backup.FileSystemID)
	}
	if backup.FileSystemARN != "arn:aws:fsx:us-east-1:123456789012:file-system/fs-windows01" {
		t.Fatalf("backup FileSystemARN = %q", backup.FileSystemARN)
	}
	if backup.SizeInBytes == nil || *backup.SizeInBytes != 4096 {
		t.Fatalf("backup size = %v, want 4096", backup.SizeInBytes)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceFSx}
}

type fakeFSxAPI struct {
	fileSystems            []awsfsxtypes.FileSystem
	backups                []awsfsxtypes.Backup
	storageVirtualMachines []awsfsxtypes.StorageVirtualMachine
	volumes                []awsfsxtypes.Volume
	snapshots              []awsfsxtypes.Snapshot
}

func (f *fakeFSxAPI) DescribeFileSystems(
	_ context.Context,
	_ *awsfsx.DescribeFileSystemsInput,
	_ ...func(*awsfsx.Options),
) (*awsfsx.DescribeFileSystemsOutput, error) {
	return &awsfsx.DescribeFileSystemsOutput{FileSystems: f.fileSystems}, nil
}

func (f *fakeFSxAPI) DescribeBackups(
	_ context.Context,
	_ *awsfsx.DescribeBackupsInput,
	_ ...func(*awsfsx.Options),
) (*awsfsx.DescribeBackupsOutput, error) {
	return &awsfsx.DescribeBackupsOutput{Backups: f.backups}, nil
}

func (f *fakeFSxAPI) DescribeStorageVirtualMachines(
	_ context.Context,
	_ *awsfsx.DescribeStorageVirtualMachinesInput,
	_ ...func(*awsfsx.Options),
) (*awsfsx.DescribeStorageVirtualMachinesOutput, error) {
	return &awsfsx.DescribeStorageVirtualMachinesOutput{StorageVirtualMachines: f.storageVirtualMachines}, nil
}

func (f *fakeFSxAPI) DescribeVolumes(
	_ context.Context,
	_ *awsfsx.DescribeVolumesInput,
	_ ...func(*awsfsx.Options),
) (*awsfsx.DescribeVolumesOutput, error) {
	return &awsfsx.DescribeVolumesOutput{Volumes: f.volumes}, nil
}

func (f *fakeFSxAPI) DescribeSnapshots(
	_ context.Context,
	_ *awsfsx.DescribeSnapshotsInput,
	_ ...func(*awsfsx.Options),
) (*awsfsx.DescribeSnapshotsOutput, error) {
	return &awsfsx.DescribeSnapshotsOutput{Snapshots: f.snapshots}, nil
}

var _ apiClient = (*fakeFSxAPI)(nil)
