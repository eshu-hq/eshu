// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storagegateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Storage Gateway metadata-only facts for one claimed account
// and region. It never activates, deletes, shuts down, or reboots a gateway,
// never refreshes a file-share cache, and never creates or deletes volumes or
// shares. It records gateway, volume, and file-share identity plus the
// dependency edges to S3 buckets, IAM roles, KMS keys, CloudWatch log groups,
// and VPC endpoints.
type Scanner struct {
	Client Client
}

// Scan observes Storage Gateway gateways, iSCSI volumes, and NFS/SMB S3 file
// shares through the configured client and returns their resource and
// relationship envelopes. Object contents, client allow lists, and admin/user
// lists stay outside the scanner contract.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("storagegateway scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceStorageGateway:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceStorageGateway
	default:
		return nil, fmt.Errorf("storagegateway scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	gateways, err := s.Client.ListGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Storage Gateway gateways: %w", err)
	}
	for _, gateway := range gateways {
		next, err := gatewayEnvelopes(boundary, gateway)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	volumes, err := s.Client.ListVolumes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Storage Gateway volumes: %w", err)
	}
	for _, volume := range volumes {
		next, err := volumeEnvelopes(boundary, volume)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	shares, err := s.Client.ListFileShares(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Storage Gateway file shares: %w", err)
	}
	for _, share := range shares {
		next, err := fileShareEnvelopes(boundary, share)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

func gatewayEnvelopes(boundary awscloud.Boundary, gateway Gateway) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(gatewayObservation(boundary, gateway))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := gatewayVPCEndpointRelationship(boundary, gateway); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func volumeEnvelopes(boundary awscloud.Boundary, volume Volume) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(volumeObservation(boundary, volume))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := volumeOnGatewayRelationship(boundary, volume); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func fileShareEnvelopes(boundary awscloud.Boundary, share FileShare) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(fileShareObservation(boundary, share))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		fileShareOnGatewayRelationship(boundary, share),
		fileShareS3BucketRelationship(boundary, share),
		fileShareRoleRelationship(boundary, share),
		fileShareKMSKeyRelationship(boundary, share),
		fileShareLogGroupRelationship(boundary, share),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func gatewayObservation(boundary awscloud.Boundary, gateway Gateway) awscloud.ResourceObservation {
	arn := strings.TrimSpace(gateway.ARN)
	id := strings.TrimSpace(gateway.ID)
	resourceID := firstNonEmpty(arn, id)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeStorageGatewayGateway,
		Name:         firstNonEmpty(gateway.Name, id),
		State:        firstNonEmpty(gateway.State, gateway.OperationalState),
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"gateway_id":               id,
			"gateway_type":             strings.TrimSpace(gateway.Type),
			"operational_state":        strings.TrimSpace(gateway.OperationalState),
			"endpoint_type":            strings.TrimSpace(gateway.EndpointType),
			"host_environment":         strings.TrimSpace(gateway.HostEnvironment),
			"timezone":                 strings.TrimSpace(gateway.Timezone),
			"ec2_instance_id":          strings.TrimSpace(gateway.EC2InstanceID),
			"ec2_instance_region":      strings.TrimSpace(gateway.EC2InstanceRegion),
			"software_version":         strings.TrimSpace(gateway.SoftwareVersion),
			"cloudwatch_log_group_arn": strings.TrimSpace(gateway.CloudWatchLogGroup),
			"vpc_endpoint":             strings.TrimSpace(gateway.VPCEndpoint),
			"network_interface_count":  gateway.NetworkInterfaceCount,
		},
		CorrelationAnchors: []string{arn, id},
		SourceRecordID:     resourceID,
	}
}

func volumeObservation(boundary awscloud.Boundary, volume Volume) awscloud.ResourceObservation {
	arn := strings.TrimSpace(volume.ARN)
	id := strings.TrimSpace(volume.ID)
	resourceID := volumeResourceID(volume)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeStorageGatewayVolume,
		Name:         firstNonEmpty(id, arn),
		State:        strings.TrimSpace(volume.AttachmentStatus),
		Attributes: map[string]any{
			"volume_id":         id,
			"volume_type":       strings.TrimSpace(volume.Type),
			"size_in_bytes":     volume.SizeInBytes,
			"attachment_status": strings.TrimSpace(volume.AttachmentStatus),
			"gateway_arn":       strings.TrimSpace(volume.GatewayARN),
			"gateway_id":        strings.TrimSpace(volume.GatewayID),
		},
		CorrelationAnchors: []string{arn, id},
		SourceRecordID:     resourceID,
	}
}

func fileShareObservation(boundary awscloud.Boundary, share FileShare) awscloud.ResourceObservation {
	arn := strings.TrimSpace(share.ARN)
	id := strings.TrimSpace(share.ID)
	resourceID := fileShareResourceID(share)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeStorageGatewayFileShare,
		Name:         firstNonEmpty(share.Name, id, arn),
		State:        strings.TrimSpace(share.Status),
		Attributes: map[string]any{
			"file_share_id":         id,
			"file_share_name":       strings.TrimSpace(share.Name),
			"protocol":              strings.TrimSpace(share.Protocol),
			"file_share_type":       strings.TrimSpace(share.Type),
			"file_share_status":     strings.TrimSpace(share.Status),
			"gateway_arn":           strings.TrimSpace(share.GatewayARN),
			"location_arn":          strings.TrimSpace(share.LocationARN),
			"bucket_region":         strings.TrimSpace(share.BucketRegion),
			"role_arn":              strings.TrimSpace(share.Role),
			"kms_key_arn":           strings.TrimSpace(share.KMSKey),
			"encryption_type":       strings.TrimSpace(share.EncryptionType),
			"default_storage_class": strings.TrimSpace(share.DefaultStorageClass),
			"object_acl":            strings.TrimSpace(share.ObjectACL),
			"audit_destination_arn": strings.TrimSpace(share.AuditDestinationARN),
			"read_only":             share.ReadOnly,
		},
		CorrelationAnchors: []string{arn, id},
		SourceRecordID:     resourceID,
	}
}
