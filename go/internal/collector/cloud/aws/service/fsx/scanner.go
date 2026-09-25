// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fsx

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS FSx metadata facts for one claimed account and region. It
// covers every FSx flavor (Windows File Server, Lustre, NetApp ONTAP, OpenZFS)
// and never persists Active Directory self-managed credentials, the ONTAP
// fsxadmin password, the SVM admin password, or file contents.
type Scanner struct {
	Client Client
}

// Scan observes FSx file systems, backups, storage virtual machines, volumes,
// and snapshots through the configured client, then emits resource facts and
// VPC, subnet, KMS-key, AD-directory, backup, SVM, and volume relationships.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("fsx scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceFSx:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceFSx
	default:
		return nil, fmt.Errorf("fsx scanner received service_kind %q", boundary.ServiceKind)
	}

	systems, err := s.Client.ListFileSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list FSx file systems: %w", err)
	}
	backups, err := s.Client.ListBackups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list FSx backups: %w", err)
	}
	svms, err := s.Client.ListStorageVirtualMachines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list FSx storage virtual machines: %w", err)
	}
	volumes, err := s.Client.ListVolumes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list FSx volumes: %w", err)
	}
	snapshots, err := s.Client.ListSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list FSx snapshots: %w", err)
	}

	fileSystemARNs := fileSystemARNMap(systems)
	svmARNs := storageVirtualMachineARNMap(svms)

	var envelopes []facts.Envelope
	for _, fs := range systems {
		resource, err := aws.NewResourceEnvelope(fileSystemObservation(boundary, fs))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(fileSystemRelationships(boundary, fs))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)
	}
	for _, svm := range svms {
		resource, err := aws.NewResourceEnvelope(svmObservation(boundary, svm))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(svmRelationships(boundary, svm, fileSystemARNs))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)
	}
	for _, volume := range volumes {
		resource, err := aws.NewResourceEnvelope(volumeObservation(boundary, volume))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(volumeRelationships(boundary, volume, svmARNs, fileSystemARNs))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)
	}
	for _, snapshot := range snapshots {
		resource, err := aws.NewResourceEnvelope(snapshotObservation(boundary, snapshot))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, backup := range backups {
		resource, err := aws.NewResourceEnvelope(backupObservation(boundary, backup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(backupRelationships(boundary, backup, fileSystemARNs))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)
	}
	return envelopes, nil
}

func relationshipEnvelopes(observations []aws.RelationshipObservation) ([]facts.Envelope, error) {
	if len(observations) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(observations))
	for _, observation := range observations {
		envelope, err := aws.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func fileSystemObservation(boundary aws.Boundary, fs FileSystem) aws.ResourceObservation {
	fsARN := strings.TrimSpace(fs.ARN)
	id := strings.TrimSpace(fs.ID)
	resourceID := firstNonEmpty(fsARN, id)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          fsARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeFSxFileSystem,
		Name:         id,
		State:        strings.TrimSpace(fs.Lifecycle),
		Tags:         cloneStringMap(fs.Tags),
		Attributes: map[string]any{
			"file_system_id":              id,
			"file_system_type":            strings.TrimSpace(fs.FileSystemType),
			"file_system_type_version":    strings.TrimSpace(fs.FileSystemTypeVersion),
			"deployment_type":             strings.TrimSpace(fs.DeploymentType),
			"storage_type":                strings.TrimSpace(fs.StorageType),
			"storage_capacity_gib":        fs.StorageCapacityGiB,
			"throughput_capacity_mbps":    int32OrNil(fs.ThroughputCapacityMBps),
			"per_unit_storage_throughput": int32OrNil(fs.PerUnitStorageThroughput),
			"owner_id":                    strings.TrimSpace(fs.OwnerID),
			"vpc_id":                      strings.TrimSpace(fs.VPCID),
			"subnet_ids":                  cloneStrings(fs.SubnetIDs),
			"preferred_subnet_id":         strings.TrimSpace(fs.PreferredSubnetID),
			"network_type":                strings.TrimSpace(fs.NetworkType),
			"kms_key_id":                  strings.TrimSpace(fs.KMSKeyID),
			"active_directory_id":         strings.TrimSpace(fs.ActiveDirectoryID),
			"dns_name":                    strings.TrimSpace(fs.DNSName),
		},
		CorrelationAnchors: []string{fsARN, id, strings.TrimSpace(fs.DNSName)},
		SourceRecordID:     resourceID,
	}
}

func svmObservation(boundary aws.Boundary, svm StorageVirtualMachine) aws.ResourceObservation {
	svmARN := strings.TrimSpace(svm.ARN)
	id := strings.TrimSpace(svm.ID)
	resourceID := firstNonEmpty(svmARN, id)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          svmARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeFSxStorageVirtualMachine,
		Name:         strings.TrimSpace(svm.Name),
		State:        strings.TrimSpace(svm.Lifecycle),
		Tags:         cloneStringMap(svm.Tags),
		Attributes: map[string]any{
			"storage_virtual_machine_id": id,
			"file_system_id":             strings.TrimSpace(svm.FileSystemID),
			"subtype":                    strings.TrimSpace(svm.Subtype),
			"uuid":                       strings.TrimSpace(svm.UUID),
			"net_bios_name":              strings.TrimSpace(svm.NetBiosName),
			"active_directory_id":        strings.TrimSpace(svm.ActiveDirectoryID),
		},
		CorrelationAnchors: []string{svmARN, id, strings.TrimSpace(svm.UUID)},
		SourceRecordID:     resourceID,
	}
}

func volumeObservation(boundary aws.Boundary, volume Volume) aws.ResourceObservation {
	volumeARN := strings.TrimSpace(volume.ARN)
	id := strings.TrimSpace(volume.ID)
	resourceID := firstNonEmpty(volumeARN, id)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          volumeARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeFSxVolume,
		Name:         strings.TrimSpace(volume.Name),
		State:        strings.TrimSpace(volume.Lifecycle),
		Tags:         cloneStringMap(volume.Tags),
		Attributes: map[string]any{
			"volume_id":                  id,
			"volume_type":                strings.TrimSpace(volume.VolumeType),
			"file_system_id":             strings.TrimSpace(volume.FileSystemID),
			"storage_virtual_machine_id": strings.TrimSpace(volume.StorageVirtualMachineID),
			"junction_path":              strings.TrimSpace(volume.JunctionPath),
			"volume_path":                strings.TrimSpace(volume.VolumePath),
			"size_in_megabytes":          int32OrNil(volume.SizeInMegabytes),
			"storage_capacity_quota_gib": int32OrNil(volume.StorageCapacityQuotaGiB),
		},
		CorrelationAnchors: []string{volumeARN, id},
		SourceRecordID:     resourceID,
	}
}

func snapshotObservation(boundary aws.Boundary, snapshot Snapshot) aws.ResourceObservation {
	snapshotARN := strings.TrimSpace(snapshot.ARN)
	id := strings.TrimSpace(snapshot.ID)
	resourceID := firstNonEmpty(snapshotARN, id)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          snapshotARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeFSxSnapshot,
		Name:         strings.TrimSpace(snapshot.Name),
		State:        strings.TrimSpace(snapshot.Lifecycle),
		Tags:         cloneStringMap(snapshot.Tags),
		Attributes: map[string]any{
			"snapshot_id": id,
			"volume_id":   strings.TrimSpace(snapshot.VolumeID),
		},
		CorrelationAnchors: []string{snapshotARN, id},
		SourceRecordID:     resourceID,
	}
}

func backupObservation(boundary aws.Boundary, backup Backup) aws.ResourceObservation {
	backupARN := strings.TrimSpace(backup.ARN)
	id := strings.TrimSpace(backup.ID)
	resourceID := firstNonEmpty(backupARN, id)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          backupARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeFSxBackup,
		Name:         id,
		State:        strings.TrimSpace(backup.Lifecycle),
		Tags:         cloneStringMap(backup.Tags),
		Attributes: map[string]any{
			"backup_id":        id,
			"backup_type":      strings.TrimSpace(backup.Type),
			"resource_type":    strings.TrimSpace(backup.ResourceType),
			"owner_id":         strings.TrimSpace(backup.OwnerID),
			"kms_key_id":       strings.TrimSpace(backup.KMSKeyID),
			"size_in_bytes":    int64OrNil(backup.SizeInBytes),
			"source_backup_id": strings.TrimSpace(backup.SourceBackupID),
			"file_system_id":   strings.TrimSpace(backup.FileSystemID),
			"volume_id":        strings.TrimSpace(backup.VolumeID),
		},
		CorrelationAnchors: []string{backupARN, id},
		SourceRecordID:     resourceID,
	}
}
