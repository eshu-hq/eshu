// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// volumeEnvelopes builds metadata-only EBS volume facts from one boundary-wide
// DescribeVolumes result. Volumes without an identity are skipped because any
// fact or edge would be unjoinable.
func volumeEnvelopes(boundary awscloud.Boundary, volume Volume) ([]facts.Envelope, error) {
	if strings.TrimSpace(volume.ID) == "" && strings.TrimSpace(volume.ARN) == "" {
		return nil, nil
	}
	resource, err := awscloud.NewResourceEnvelope(volumeObservation(boundary, volume))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := volumeKMSRelationship(boundary, volume); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func volumeObservation(boundary awscloud.Boundary, volume Volume) awscloud.ResourceObservation {
	volumeID := strings.TrimSpace(volume.ID)
	volumeARN := strings.TrimSpace(volume.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          volumeARN,
		ResourceID:   volumeID,
		ResourceType: awscloud.ResourceTypeEC2Volume,
		Name:         volumeID,
		State:        strings.TrimSpace(volume.State),
		Tags:         volume.Tags,
		Attributes: map[string]any{
			"attachments":                volumeAttachmentMaps(volume.Attachments),
			"attachment_count":           len(volume.Attachments),
			"availability_zone":          strings.TrimSpace(volume.AvailabilityZone),
			"availability_zone_id":       strings.TrimSpace(volume.AvailabilityZoneID),
			"create_time":                timeOrNil(volume.CreateTime),
			"encrypted":                  boolValue(volume.Encrypted),
			"fast_restored":              boolValue(volume.FastRestored),
			"iops":                       int32Value(volume.IOPS),
			"kms_key_id":                 strings.TrimSpace(volume.KMSKeyID),
			"multi_attach_enabled":       boolValue(volume.MultiAttachEnabled),
			"outpost_arn":                strings.TrimSpace(volume.OutpostARN),
			"size_gib":                   int32Value(volume.SizeGiB),
			"snapshot_id":                strings.TrimSpace(volume.SnapshotID),
			"source_volume_id":           strings.TrimSpace(volume.SourceVolumeID),
			"sse_type":                   strings.TrimSpace(volume.SSEType),
			"throughput_mibps":           int32Value(volume.ThroughputMiBps),
			"volume_initialization_rate": int32Value(volume.VolumeInitializationRate),
			"volume_type":                strings.TrimSpace(volume.VolumeType),
		},
		CorrelationAnchors: []string{volumeARN, volumeID},
		SourceRecordID:     volumeID,
	}
}

func volumeKMSRelationship(boundary awscloud.Boundary, volume Volume) (awscloud.RelationshipObservation, bool) {
	volumeID := strings.TrimSpace(volume.ID)
	keyID := strings.TrimSpace(volume.KMSKeyID)
	if volumeID == "" || keyID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetARN := ""
	if isARN(keyID) {
		targetARN = keyID
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEC2VolumeUsesKMSKey,
		SourceResourceID: volumeID,
		SourceARN:        strings.TrimSpace(volume.ARN),
		TargetResourceID: keyID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   volumeID + "#kms#" + keyID,
	}, true
}

func volumeAttachmentMaps(attachments []VolumeAttachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		output = append(output, map[string]any{
			"associated_resource":     strings.TrimSpace(attachment.AssociatedResource),
			"attach_time":             timeOrNil(attachment.AttachTime),
			"delete_on_termination":   attachment.DeleteOnTermination,
			"device":                  strings.TrimSpace(attachment.Device),
			"ebs_card_index":          int32Value(attachment.EBSCardIndex),
			"instance_id":             strings.TrimSpace(attachment.InstanceID),
			"instance_owning_service": strings.TrimSpace(attachment.InstanceOwningService),
			"state":                   strings.TrimSpace(attachment.State),
			"volume_id":               strings.TrimSpace(attachment.VolumeID),
		})
	}
	return output
}

func boolValue(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}
