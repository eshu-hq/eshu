// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fsx

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsAllFlavorsAndRelationships(t *testing.T) {
	winARN := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-windows01"
	lustreARN := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-lustre01"
	ontapARN := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-ontap01"
	zfsARN := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-zfs01"
	svmARN := "arn:aws:fsx:us-east-1:123456789012:storage-virtual-machine/svm-0001"
	ontapVolARN := "arn:aws:fsx:us-east-1:123456789012:volume/fsvol-ontap01"
	zfsVolARN := "arn:aws:fsx:us-east-1:123456789012:volume/fsvol-zfs01"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd"
	tput := int32(512)
	perUnit := int32(125)
	volSize := int32(102400)
	zfsQuota := int32(100)
	backupSize := int64(4096)

	client := fakeClient{
		fileSystems: []FileSystem{
			{
				ID:                     "fs-windows01",
				ARN:                    winARN,
				FileSystemType:         "WINDOWS",
				StorageType:            "SSD",
				Lifecycle:              "AVAILABLE",
				OwnerID:                "123456789012",
				VPCID:                  "vpc-aaa",
				SubnetIDs:              []string{"subnet-1", "subnet-2"},
				PreferredSubnetID:      "subnet-1",
				StorageCapacityGiB:     2048,
				KMSKeyID:               kmsARN,
				DNSName:                "fs-windows01.example.com",
				ActiveDirectoryID:      "d-1234567890",
				DeploymentType:         "MULTI_AZ_1",
				ThroughputCapacityMBps: &tput,
				Tags:                   map[string]string{"Environment": "prod"},
			},
			{
				ID:                       "fs-lustre01",
				ARN:                      lustreARN,
				FileSystemType:           "LUSTRE",
				StorageType:              "SSD",
				Lifecycle:                "AVAILABLE",
				VPCID:                    "vpc-aaa",
				SubnetIDs:                []string{"subnet-1"},
				StorageCapacityGiB:       1200,
				DeploymentType:           "PERSISTENT_2",
				PerUnitStorageThroughput: &perUnit,
				FileSystemTypeVersion:    "2.15",
			},
			{
				ID:                     "fs-ontap01",
				ARN:                    ontapARN,
				FileSystemType:         "ONTAP",
				StorageType:            "SSD",
				Lifecycle:              "AVAILABLE",
				VPCID:                  "vpc-aaa",
				SubnetIDs:              []string{"subnet-1", "subnet-2"},
				PreferredSubnetID:      "subnet-1",
				StorageCapacityGiB:     1024,
				KMSKeyID:               kmsARN,
				DeploymentType:         "MULTI_AZ_1",
				ThroughputCapacityMBps: &tput,
			},
			{
				ID:                     "fs-zfs01",
				ARN:                    zfsARN,
				FileSystemType:         "OPENZFS",
				StorageType:            "SSD",
				Lifecycle:              "AVAILABLE",
				VPCID:                  "vpc-aaa",
				SubnetIDs:              []string{"subnet-1"},
				StorageCapacityGiB:     64,
				KMSKeyID:               kmsARN,
				DeploymentType:         "SINGLE_AZ_2",
				ThroughputCapacityMBps: &tput,
			},
		},
		storageVirtualMachines: []StorageVirtualMachine{{
			ID:                "svm-0001",
			ARN:               svmARN,
			Name:              "svm1",
			FileSystemID:      "fs-ontap01",
			Lifecycle:         "CREATED",
			Subtype:           "DEFAULT",
			UUID:              "uuid-1",
			NetBiosName:       "SVM1",
			ActiveDirectoryID: "d-0987654321",
		}},
		volumes: []Volume{
			{
				ID:                      "fsvol-ontap01",
				ARN:                     ontapVolARN,
				Name:                    "ontapvol",
				FileSystemID:            "fs-ontap01",
				VolumeType:              "ONTAP",
				Lifecycle:               "CREATED",
				StorageVirtualMachineID: "svm-0001",
				JunctionPath:            "/vol1",
				SizeInMegabytes:         &volSize,
			},
			{
				ID:                      "fsvol-zfs01",
				ARN:                     zfsVolARN,
				Name:                    "zfsvol",
				FileSystemID:            "fs-zfs01",
				VolumeType:              "OPENZFS",
				Lifecycle:               "CREATED",
				VolumePath:              "/fsx/zfs",
				StorageCapacityQuotaGiB: &zfsQuota,
			},
		},
		snapshots: []Snapshot{{
			ID:        "fsvolsnap-0001",
			ARN:       "arn:aws:fsx:us-east-1:123456789012:snapshot/fsvolsnap-0001",
			Name:      "daily",
			VolumeID:  "fsvol-zfs01",
			Lifecycle: "AVAILABLE",
		}},
		backups: []Backup{{
			ID:            "backup-0001",
			ARN:           "arn:aws:fsx:us-east-1:123456789012:backup/backup-0001",
			Type:          "AUTOMATIC",
			Lifecycle:     "AVAILABLE",
			OwnerID:       "123456789012",
			KMSKeyID:      kmsARN,
			SizeInBytes:   &backupSize,
			ResourceType:  "FILE_SYSTEM",
			FileSystemID:  "fs-windows01",
			FileSystemARN: winARN,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Four file systems, one per flavor.
	if got, want := countResources(envelopes, awscloud.ResourceTypeFSxFileSystem), 4; got != want {
		t.Fatalf("file system resources = %d, want %d", got, want)
	}
	win := resourceByID(t, envelopes, winARN)
	winAttrs := attributesOf(t, win)
	if got, want := winAttrs["file_system_type"], "WINDOWS"; got != want {
		t.Fatalf("windows file_system_type = %#v, want %q", got, want)
	}
	if got, want := winAttrs["deployment_type"], "MULTI_AZ_1"; got != want {
		t.Fatalf("windows deployment_type = %#v, want %q", got, want)
	}
	if got, want := winAttrs["throughput_capacity_mbps"], int32(512); got != want {
		t.Fatalf("windows throughput_capacity_mbps = %#v, want %v", got, want)
	}
	if got, want := winAttrs["storage_capacity_gib"], int32(2048); got != want {
		t.Fatalf("windows storage_capacity_gib = %#v, want %v", got, want)
	}

	lustre := resourceByID(t, envelopes, lustreARN)
	lustreAttrs := attributesOf(t, lustre)
	if got, want := lustreAttrs["file_system_type"], "LUSTRE"; got != want {
		t.Fatalf("lustre file_system_type = %#v, want %q", got, want)
	}
	if got, want := lustreAttrs["per_unit_storage_throughput"], int32(125); got != want {
		t.Fatalf("lustre per_unit_storage_throughput = %#v, want %v", got, want)
	}
	if got, want := lustreAttrs["file_system_type_version"], "2.15"; got != want {
		t.Fatalf("lustre file_system_type_version = %#v, want %q", got, want)
	}

	resourceByID(t, envelopes, ontapARN)
	resourceByID(t, envelopes, zfsARN)

	// SVM, volumes, snapshot, backup resources.
	if got, want := countResources(envelopes, awscloud.ResourceTypeFSxStorageVirtualMachine), 1; got != want {
		t.Fatalf("svm resources = %d, want %d", got, want)
	}
	if got, want := countResources(envelopes, awscloud.ResourceTypeFSxVolume), 2; got != want {
		t.Fatalf("volume resources = %d, want %d", got, want)
	}
	if got, want := countResources(envelopes, awscloud.ResourceTypeFSxSnapshot), 1; got != want {
		t.Fatalf("snapshot resources = %d, want %d", got, want)
	}
	if got, want := countResources(envelopes, awscloud.ResourceTypeFSxBackup), 1; got != want {
		t.Fatalf("backup resources = %d, want %d", got, want)
	}

	// Relationships: VPC (x4), subnet (2+1+2+1=6), KMS (windows, ontap, zfs =3),
	// AD-directory file system (windows =1), SVM->file system (1), SVM->AD (1),
	// volume->SVM (1, ontap only), volume->file system (2), backup->file system (1).
	assertRelationship(t, envelopes, awscloud.RelationshipFSxFileSystemInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxFileSystemInSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxFileSystemUsesKMSKey)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxFileSystemUsesADDirectory)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxSVMTargetsFileSystem)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxSVMUsesADDirectory)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxVolumeTargetsSVM)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxVolumeTargetsFileSystem)
	assertRelationship(t, envelopes, awscloud.RelationshipFSxBackupTargetsFileSystem)

	if got, want := countRelationships(envelopes, awscloud.RelationshipFSxFileSystemInVPC), 4; got != want {
		t.Fatalf("file-system-in-vpc relationships = %d, want %d", got, want)
	}
	if got, want := countRelationships(envelopes, awscloud.RelationshipFSxFileSystemInSubnet), 6; got != want {
		t.Fatalf("file-system-in-subnet relationships = %d, want %d", got, want)
	}
	if got, want := countRelationships(envelopes, awscloud.RelationshipFSxFileSystemUsesKMSKey), 3; got != want {
		t.Fatalf("file-system-uses-kms relationships = %d, want %d", got, want)
	}
	if got, want := countRelationships(envelopes, awscloud.RelationshipFSxVolumeTargetsSVM), 1; got != want {
		t.Fatalf("volume-targets-svm relationships = %d, want %d", got, want)
	}

	// Every relationship must carry a non-empty target_type and target_resource_id.
	assertRelationshipJoinKeys(t, envelopes)

	// Graph-join: the SVM->file system edge upgrades the bare file system ID to
	// the ONTAP file system ARN so it joins the file system resource fact.
	svmFSEdge := relationshipByType(t, envelopes, awscloud.RelationshipFSxSVMTargetsFileSystem)
	if got, want := svmFSEdge.Payload["target_resource_id"], ontapARN; got != want {
		t.Fatalf("svm->file-system target_resource_id = %#v, want ARN %q", got, want)
	}
	if got, want := svmFSEdge.Payload["target_type"], awscloud.ResourceTypeFSxFileSystem; got != want {
		t.Fatalf("svm->file-system target_type = %#v, want %q", got, want)
	}

	// Volume->SVM edge upgrades to the SVM ARN.
	volSVMEdge := relationshipByType(t, envelopes, awscloud.RelationshipFSxVolumeTargetsSVM)
	if got, want := volSVMEdge.Payload["target_resource_id"], svmARN; got != want {
		t.Fatalf("volume->svm target_resource_id = %#v, want ARN %q", got, want)
	}

	// VPC and subnet edges target the bare AWS ID (joins aws_ec2_vpc / aws_ec2_subnet).
	vpcEdge := relationshipByType(t, envelopes, awscloud.RelationshipFSxFileSystemInVPC)
	if got, want := vpcEdge.Payload["target_resource_id"], "vpc-aaa"; got != want {
		t.Fatalf("file-system->vpc target_resource_id = %#v, want bare %q", got, want)
	}
	if got, want := vpcEdge.Payload["target_type"], awscloud.ResourceTypeEC2VPC; got != want {
		t.Fatalf("file-system->vpc target_type = %#v, want %q", got, want)
	}

	// AD edges target the bare directory ID (joins aws_ds_directory).
	adEdge := relationshipByType(t, envelopes, awscloud.RelationshipFSxFileSystemUsesADDirectory)
	if got, want := adEdge.Payload["target_resource_id"], "d-1234567890"; got != want {
		t.Fatalf("file-system->ad target_resource_id = %#v, want %q", got, want)
	}
	if got, want := adEdge.Payload["target_type"], awscloud.ResourceTypeDSDirectory; got != want {
		t.Fatalf("file-system->ad target_type = %#v, want %q", got, want)
	}

	// KMS edge ARN-shaped: target_arn populated.
	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipFSxFileSystemUsesKMSKey)
	if got, want := kmsEdge.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("file-system->kms target_arn = %#v, want %q", got, want)
	}
	if got, want := kmsEdge.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("file-system->kms target_type = %#v, want %q", got, want)
	}
}

// TestScannerNeverPersistsCredentialsAcrossFlavors proves the scanner never
// stores Active Directory self-managed credentials (Windows + ONTAP), the SVM
// admin password, the ONTAP fsxadmin password, DNS server IPs, the AD
// service-account user name, or file contents. The scanner-owned types have no
// field for these values; this test guards against a future regression that
// might smuggle them into an attribute map.
func TestScannerNeverPersistsCredentialsAcrossFlavors(t *testing.T) {
	forbidden := []string{
		"password", "admin_password", "fsx_admin_password", "svm_admin_password",
		"self_managed_active_directory", "domain_join_service_account_secret",
		"user_name", "username", "service_account", "dns_ips", "domain_password",
		"file_system_administrators_group", "secret", "credentials",
	}

	client := fakeClient{
		fileSystems: []FileSystem{
			{
				ID: "fs-windows01", ARN: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-windows01",
				FileSystemType: "WINDOWS", Lifecycle: "AVAILABLE", VPCID: "vpc-aaa",
				SubnetIDs: []string{"subnet-1"}, ActiveDirectoryID: "d-1234567890",
			},
			{
				ID: "fs-ontap01", ARN: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-ontap01",
				FileSystemType: "ONTAP", Lifecycle: "AVAILABLE", VPCID: "vpc-aaa",
				SubnetIDs: []string{"subnet-1"}, ActiveDirectoryID: "d-1234567890",
			},
			{
				ID: "fs-lustre01", ARN: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-lustre01",
				FileSystemType: "LUSTRE", Lifecycle: "AVAILABLE", VPCID: "vpc-aaa", SubnetIDs: []string{"subnet-1"},
			},
			{
				ID: "fs-zfs01", ARN: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-zfs01",
				FileSystemType: "OPENZFS", Lifecycle: "AVAILABLE", VPCID: "vpc-aaa", SubnetIDs: []string{"subnet-1"},
			},
		},
		storageVirtualMachines: []StorageVirtualMachine{{
			ID: "svm-0001", ARN: "arn:aws:fsx:us-east-1:123456789012:storage-virtual-machine/svm-0001",
			Name: "svm1", FileSystemID: "fs-ontap01", Lifecycle: "CREATED",
			NetBiosName: "SVM1", ActiveDirectoryID: "d-0987654321",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attributes, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			continue
		}
		for key := range attributes {
			for _, bad := range forbidden {
				if key == bad {
					t.Fatalf("attribute %q persisted on %v; FSx scanner must never store AD/SVM credentials or secrets",
						key, envelope.Payload["resource_type"])
				}
			}
		}
	}
}

// TestBackupRelationshipKeepsARNWhenFileSystemOutOfScope proves the
// backup->file system edge keeps the file system ARN as target_resource_id when
// the backup carries the source ARN but the file system is not in the current
// scan's ARN map (a cross-region/cross-slice backup, or one whose source file
// system has been deleted). The bare file system ID must not overwrite the ARN,
// because the file system resource fact's resource_id is the ARN; downgrading to
// the bare ID would dangle the edge.
func TestBackupRelationshipKeepsARNWhenFileSystemOutOfScope(t *testing.T) {
	const crossRegionFSARN = "arn:aws:fsx:us-west-2:123456789012:file-system/fs-elsewhere"

	backup := Backup{
		ID:            "backup-xregion",
		ARN:           "arn:aws:fsx:us-east-1:123456789012:backup/backup-xregion",
		FileSystemID:  "fs-elsewhere",
		FileSystemARN: crossRegionFSARN,
	}
	// The current scan only knows an unrelated, in-region file system, so the
	// backup's source file system is out of scope.
	fileSystemARNs := map[string]string{
		"fs-local": "arn:aws:fsx:us-east-1:123456789012:file-system/fs-local",
	}

	relationships := backupRelationships(testBoundary(), backup, fileSystemARNs)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("backupRelationships() len = %d, want %d", got, want)
	}
	edge := relationships[0]
	if got, want := edge.TargetResourceID, crossRegionFSARN; got != want {
		t.Fatalf("backup->file-system target_resource_id = %q, want ARN %q (bare ID must not overwrite the ARN)", got, want)
	}
	if got, want := edge.TargetARN, crossRegionFSARN; got != want {
		t.Fatalf("backup->file-system target_arn = %q, want ARN %q", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSQS

	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := (Scanner{Client: fakeClient{
		fileSystems: []FileSystem{{ID: "fs-1", ARN: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-1", Lifecycle: "AVAILABLE"}},
	}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countResources(envelopes, awscloud.ResourceTypeFSxFileSystem); got != 1 {
		t.Fatalf("file system resources = %d, want 1", got)
	}
}
