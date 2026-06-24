// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fsx

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// fileSystemRelationships returns the VPC, subnet, KMS-key, and AD-directory
// edges reported by one file system. Each edge sets a non-empty target_type and
// a target_resource_id that matches the target scanner's resource_id: VPC and
// subnet edges use the bare AWS ID, KMS edges use the key ARN or ID, and AD
// edges use the bare directory ID.
func fileSystemRelationships(boundary awscloud.Boundary, fs FileSystem) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(fs.ARN, fs.ID)
	if sourceID == "" {
		return nil
	}
	fsARN := strings.TrimSpace(fs.ARN)
	var relationships []awscloud.RelationshipObservation

	if vpcID := strings.TrimSpace(fs.VPCID); vpcID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxFileSystemInVPC,
			SourceResourceID: sourceID,
			SourceARN:        fsARN,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxFileSystemInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(fs.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxFileSystemInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        fsARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"subnet_id": subnetID,
				"preferred": subnetID == strings.TrimSpace(fs.PreferredSubnetID),
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipFSxFileSystemInSubnet, subnetID),
		})
	}
	if kmsKey := strings.TrimSpace(fs.KMSKeyID); kmsKey != "" {
		relationships = append(relationships, kmsKeyRelationship(
			boundary, awscloud.RelationshipFSxFileSystemUsesKMSKey, sourceID, fsARN, kmsKey,
		))
	}
	if directoryID := strings.TrimSpace(fs.ActiveDirectoryID); directoryID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxFileSystemUsesADDirectory,
			SourceResourceID: sourceID,
			SourceARN:        fsARN,
			TargetResourceID: directoryID,
			TargetType:       awscloud.ResourceTypeDSDirectory,
			Attributes:       map[string]any{"directory_id": directoryID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxFileSystemUsesADDirectory, directoryID),
		})
	}
	return relationships
}

// svmRelationships returns the file-system and AD-directory edges for one ONTAP
// storage virtual machine. The file-system edge is upgraded to the file system
// ARN when the SVM's parent file system is known, so it joins the file system
// resource fact by ARN.
func svmRelationships(
	boundary awscloud.Boundary,
	svm StorageVirtualMachine,
	fileSystemARNs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(svm.ARN, svm.ID)
	if sourceID == "" {
		return nil
	}
	svmARN := strings.TrimSpace(svm.ARN)
	var relationships []awscloud.RelationshipObservation

	if fsID := strings.TrimSpace(svm.FileSystemID); fsID != "" {
		targetID, targetARN := fileSystemTarget(fsID, fileSystemARNs)
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxSVMTargetsFileSystem,
			SourceResourceID: sourceID,
			SourceARN:        svmARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeFSxFileSystem,
			Attributes:       map[string]any{"file_system_id": fsID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxSVMTargetsFileSystem, targetID),
		})
	}
	if directoryID := strings.TrimSpace(svm.ActiveDirectoryID); directoryID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxSVMUsesADDirectory,
			SourceResourceID: sourceID,
			SourceARN:        svmARN,
			TargetResourceID: directoryID,
			TargetType:       awscloud.ResourceTypeDSDirectory,
			Attributes:       map[string]any{"directory_id": directoryID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxSVMUsesADDirectory, directoryID),
		})
	}
	return relationships
}

// volumeRelationships returns the SVM (ONTAP) and file-system edges for one
// volume. The targets are upgraded to ARNs when the parent SVM or file system
// is known so the edges join the SVM and file system resource facts by ARN.
func volumeRelationships(
	boundary awscloud.Boundary,
	volume Volume,
	storageVirtualMachineARNs map[string]string,
	fileSystemARNs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(volume.ARN, volume.ID)
	if sourceID == "" {
		return nil
	}
	volumeARN := strings.TrimSpace(volume.ARN)
	var relationships []awscloud.RelationshipObservation

	if svmID := strings.TrimSpace(volume.StorageVirtualMachineID); svmID != "" {
		targetID := svmID
		targetARN := ""
		if arn, ok := storageVirtualMachineARNs[svmID]; ok && arn != "" {
			targetID = arn
			targetARN = arn
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxVolumeTargetsSVM,
			SourceResourceID: sourceID,
			SourceARN:        volumeARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeFSxStorageVirtualMachine,
			Attributes:       map[string]any{"storage_virtual_machine_id": svmID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxVolumeTargetsSVM, targetID),
		})
	}
	if fsID := strings.TrimSpace(volume.FileSystemID); fsID != "" {
		targetID, targetARN := fileSystemTarget(fsID, fileSystemARNs)
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFSxVolumeTargetsFileSystem,
			SourceResourceID: sourceID,
			SourceARN:        volumeARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeFSxFileSystem,
			Attributes:       map[string]any{"file_system_id": fsID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxVolumeTargetsFileSystem, targetID),
		})
	}
	return relationships
}

// backupRelationships returns the source-file-system edge for one backup. The
// target is upgraded to the file system ARN when the source file system is in
// this scan's ARN map so the edge joins the file system resource fact by ARN.
// When the backup reports a source file system ARN but that file system is not
// in scope (a cross-region/cross-slice backup, or a deleted source file system),
// the edge keeps the reported ARN as both target_resource_id and target_arn so a
// later projection can still resolve it by ARN; it is never downgraded to the
// bare file system ID.
func backupRelationships(
	boundary awscloud.Boundary,
	backup Backup,
	fileSystemARNs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(backup.ARN, backup.ID)
	fsID := firstNonEmpty(backup.FileSystemARN, backup.FileSystemID)
	if sourceID == "" || fsID == "" {
		return nil
	}
	// targetID starts at the source ARN when the backup reports one (firstNonEmpty
	// prefers FileSystemARN), so an out-of-scope file system still joins by ARN.
	targetID := fsID
	targetARN := strings.TrimSpace(backup.FileSystemARN)
	// Only upgrade to an in-scope ARN; never downgrade an already-ARN target back
	// to the bare ID when the source file system is not in this scan's map (e.g.
	// a cross-region/cross-slice backup or a deleted source file system).
	if rawID := strings.TrimSpace(backup.FileSystemID); rawID != "" {
		if _, arn := fileSystemTarget(rawID, fileSystemARNs); arn != "" {
			targetID = arn
			targetARN = arn
		}
	}
	if isARN(targetID) {
		targetARN = targetID
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFSxBackupTargetsFileSystem,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(backup.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeFSxFileSystem,
		Attributes:       map[string]any{"file_system_id": strings.TrimSpace(backup.FileSystemID)},
		SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipFSxBackupTargetsFileSystem, targetID),
	}}
}

// kmsKeyRelationship builds a KMS-key edge. The target_resource_id is the key
// ARN or ID exactly as FSx reports it, matching the KMS scanner's resource_id;
// target_arn is set only when the value is ARN-shaped.
func kmsKeyRelationship(
	boundary awscloud.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	kmsKey string,
) awscloud.RelationshipObservation {
	targetARN := ""
	if isARN(kmsKey) {
		targetARN = kmsKey
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: kmsKey,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   relationshipRecordID(sourceID, relationshipType, kmsKey),
	}
}

// fileSystemTarget upgrades a bare file system ID to its ARN when the ARN is
// known, so file-system edges join the file system resource fact (whose
// resource_id is the ARN). It returns the chosen target ID and the ARN (empty
// when only the bare ID is available).
func fileSystemTarget(fsID string, fileSystemARNs map[string]string) (string, string) {
	if arn, ok := fileSystemARNs[fsID]; ok && arn != "" {
		return arn, arn
	}
	return fsID, ""
}

func fileSystemARNMap(systems []FileSystem) map[string]string {
	arns := make(map[string]string, len(systems))
	for _, fs := range systems {
		id := strings.TrimSpace(fs.ID)
		if id == "" {
			continue
		}
		arns[id] = strings.TrimSpace(fs.ARN)
	}
	return arns
}

func storageVirtualMachineARNMap(svms []StorageVirtualMachine) map[string]string {
	arns := make(map[string]string, len(svms))
	for _, svm := range svms {
		id := strings.TrimSpace(svm.ID)
		if id == "" {
			continue
		}
		arns[id] = strings.TrimSpace(svm.ARN)
	}
	return arns
}
