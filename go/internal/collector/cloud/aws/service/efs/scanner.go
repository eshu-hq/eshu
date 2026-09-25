// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package efs

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS EFS metadata facts for one claimed account and region. It
// never reads file contents and never persists NFS file system policy bodies.
type Scanner struct {
	Client Client
}

// Scan observes EFS file systems, access points, mount targets, and
// replication configurations through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("efs scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceEFS:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceEFS
	default:
		return nil, fmt.Errorf("efs scanner received service_kind %q", boundary.ServiceKind)
	}

	systems, err := s.Client.ListFileSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EFS file systems: %w", err)
	}
	replications, err := s.Client.ListReplicationConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EFS replication configurations: %w", err)
	}

	var envelopes []facts.Envelope
	for _, system := range systems {
		systemEnvelopes, err := fileSystemEnvelopes(boundary, system)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, systemEnvelopes...)
	}
	for _, replication := range replications {
		replicationEnvelopes, err := replicationEnvelopes(boundary, replication)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, replicationEnvelopes...)
	}
	return envelopes, nil
}

func fileSystemEnvelopes(boundary aws.Boundary, system FileSystem) ([]facts.Envelope, error) {
	fsKey := firstNonEmpty(system.ARN, system.ID)
	resource, err := aws.NewResourceEnvelope(fileSystemObservation(boundary, system))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	if kmsKey := strings.TrimSpace(system.KMSKeyID); system.Encrypted && kmsKey != "" {
		relationship, err := aws.NewRelationshipEnvelope(aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEFSFileSystemUsesKMSKey,
			SourceResourceID: fsKey,
			SourceARN:        system.ARN,
			TargetResourceID: kmsKey,
			TargetARN:        kmsKey,
			TargetType:       aws.ResourceTypeKMSKey,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}

	for _, accessPoint := range system.AccessPoints {
		accessPointEnvelopes, err := accessPointEnvelopes(boundary, fsKey, system.ARN, accessPoint)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, accessPointEnvelopes...)
	}
	for _, mountTarget := range system.MountTargets {
		mountTargetEnvelopes, err := mountTargetEnvelopes(boundary, mountTarget)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, mountTargetEnvelopes...)
	}
	return envelopes, nil
}

func accessPointEnvelopes(
	boundary aws.Boundary,
	fileSystemKey string,
	fileSystemARN string,
	accessPoint AccessPoint,
) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(accessPointObservation(boundary, accessPoint))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	apKey := firstNonEmpty(accessPoint.ARN, accessPoint.ID)
	if apKey != "" && fileSystemKey != "" {
		relationship, err := aws.NewRelationshipEnvelope(aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEFSAccessPointTargetsFileSystem,
			SourceResourceID: apKey,
			SourceARN:        accessPoint.ARN,
			TargetResourceID: fileSystemKey,
			TargetARN:        fileSystemARN,
			TargetType:       aws.ResourceTypeEFSFileSystem,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func mountTargetEnvelopes(boundary aws.Boundary, mountTarget MountTarget) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(mountTargetObservation(boundary, mountTarget))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	mtID := strings.TrimSpace(mountTarget.ID)
	if subnet := strings.TrimSpace(mountTarget.SubnetID); mtID != "" && subnet != "" {
		relationship, err := aws.NewRelationshipEnvelope(aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEFSMountTargetInSubnet,
			SourceResourceID: mtID,
			TargetResourceID: subnet,
			TargetType:       aws.ResourceTypeEC2Subnet,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	for _, securityGroup := range mountTarget.SecurityGroupIDs {
		group := strings.TrimSpace(securityGroup)
		if mtID == "" || group == "" {
			continue
		}
		relationship, err := aws.NewRelationshipEnvelope(aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEFSMountTargetUsesSecurityGroup,
			SourceResourceID: mtID,
			TargetResourceID: group,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func replicationEnvelopes(boundary aws.Boundary, replication ReplicationConfiguration) ([]facts.Envelope, error) {
	sourceKey := firstNonEmpty(replication.SourceFileSystemARN, replication.SourceFileSystemID)
	if sourceKey == "" {
		return nil, nil
	}
	resource, err := aws.NewResourceEnvelope(replicationObservation(boundary, replication))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	for _, destination := range replication.Destinations {
		target := strings.TrimSpace(destination.FileSystemID)
		if target == "" {
			continue
		}
		relationship, err := aws.NewRelationshipEnvelope(aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEFSReplicationTargetsFileSystem,
			SourceResourceID: sourceKey,
			SourceARN:        replication.SourceFileSystemARN,
			TargetResourceID: target,
			TargetType:       aws.ResourceTypeEFSFileSystem,
			Attributes: map[string]any{
				"destination_region": strings.TrimSpace(destination.Region),
				"replication_status": strings.TrimSpace(destination.Status),
			},
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func fileSystemObservation(boundary aws.Boundary, system FileSystem) aws.ResourceObservation {
	fsARN := strings.TrimSpace(system.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          fsARN,
		ResourceID:   firstNonEmpty(fsARN, system.ID),
		ResourceType: aws.ResourceTypeEFSFileSystem,
		Name:         strings.TrimSpace(system.Name),
		State:        strings.TrimSpace(system.LifeCycleState),
		Tags:         cloneStringMap(system.Tags),
		Attributes: map[string]any{
			"file_system_id":                          strings.TrimSpace(system.ID),
			"owner_id":                                strings.TrimSpace(system.OwnerID),
			"performance_mode":                        strings.TrimSpace(system.PerformanceMode),
			"throughput_mode":                         strings.TrimSpace(system.ThroughputMode),
			"encrypted":                               system.Encrypted,
			"kms_key_id":                              strings.TrimSpace(system.KMSKeyID),
			"availability_zone_id":                    strings.TrimSpace(system.AvailabilityZoneID),
			"number_of_mount_targets":                 system.NumberOfMountTargets,
			"lifecycle_transition_to_ia":              strings.TrimSpace(system.LifecyclePolicy.TransitionToIA),
			"lifecycle_transition_to_archive":         strings.TrimSpace(system.LifecyclePolicy.TransitionToArchive),
			"lifecycle_transition_to_primary_storage": strings.TrimSpace(system.LifecyclePolicy.TransitionToPrimaryStorageClass),
		},
		CorrelationAnchors: []string{fsARN, system.ID},
		SourceRecordID:     firstNonEmpty(fsARN, system.ID),
	}
}

func accessPointObservation(boundary aws.Boundary, accessPoint AccessPoint) aws.ResourceObservation {
	apARN := strings.TrimSpace(accessPoint.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          apARN,
		ResourceID:   firstNonEmpty(apARN, accessPoint.ID),
		ResourceType: aws.ResourceTypeEFSAccessPoint,
		Name:         strings.TrimSpace(accessPoint.Name),
		State:        strings.TrimSpace(accessPoint.LifeCycleState),
		Tags:         cloneStringMap(accessPoint.Tags),
		Attributes: map[string]any{
			"access_point_id": strings.TrimSpace(accessPoint.ID),
			"file_system_id":  strings.TrimSpace(accessPoint.FileSystemID),
			"root_directory":  strings.TrimSpace(accessPoint.RootDirectory),
			"posix_uid":       int64OrNil(accessPoint.PosixUID),
			"posix_gid":       int64OrNil(accessPoint.PosixGID),
		},
		CorrelationAnchors: []string{apARN, accessPoint.ID},
		SourceRecordID:     firstNonEmpty(apARN, accessPoint.ID),
	}
}

func mountTargetObservation(boundary aws.Boundary, mountTarget MountTarget) aws.ResourceObservation {
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   strings.TrimSpace(mountTarget.ID),
		ResourceType: aws.ResourceTypeEFSMountTarget,
		State:        strings.TrimSpace(mountTarget.LifeCycleState),
		Attributes: map[string]any{
			"mount_target_id":      strings.TrimSpace(mountTarget.ID),
			"file_system_id":       strings.TrimSpace(mountTarget.FileSystemID),
			"subnet_id":            strings.TrimSpace(mountTarget.SubnetID),
			"vpc_id":               strings.TrimSpace(mountTarget.VPCID),
			"availability_zone_id": strings.TrimSpace(mountTarget.AvailabilityZoneID),
			"ip_address":           strings.TrimSpace(mountTarget.IPAddress),
			"network_interface_id": strings.TrimSpace(mountTarget.NetworkInterfaceID),
			"security_group_ids":   cloneStrings(mountTarget.SecurityGroupIDs),
		},
		CorrelationAnchors: []string{mountTarget.ID},
		SourceRecordID:     strings.TrimSpace(mountTarget.ID),
	}
}

func replicationObservation(boundary aws.Boundary, replication ReplicationConfiguration) aws.ResourceObservation {
	sourceARN := strings.TrimSpace(replication.SourceFileSystemARN)
	sourceID := strings.TrimSpace(replication.SourceFileSystemID)
	resourceID := firstNonEmpty(sourceARN, sourceID)
	destinations := make([]map[string]any, 0, len(replication.Destinations))
	for _, destination := range replication.Destinations {
		destinations = append(destinations, map[string]any{
			"file_system_id":     strings.TrimSpace(destination.FileSystemID),
			"destination_region": strings.TrimSpace(destination.Region),
			"replication_status": strings.TrimSpace(destination.Status),
		})
	}
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          sourceARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeEFSReplicationConfiguration,
		Attributes: map[string]any{
			"source_file_system_id": sourceID,
			"destinations":          destinations,
		},
		CorrelationAnchors: []string{sourceARN, sourceID},
		SourceRecordID:     resourceID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func int64OrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
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
