// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dms

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Database Migration Service metadata-only facts for one
// claimed account and region. It reads control-plane describe APIs and resource
// tags only and never reads migrated rows, endpoint credentials, task settings,
// or table-mapping bodies, and never mutates DMS state. It reports replication
// instances, replication subnet groups, endpoints, and replication tasks plus
// the placement and data-store dependency relationships each one reports.
type Scanner struct {
	// Client is the metadata-only DMS snapshot source.
	Client Client
}

// Scan observes DMS replication instances, subnet groups, endpoints, and
// replication tasks plus their direct EC2, KMS, S3, Kinesis, Secrets Manager,
// and intra-DMS dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("dms scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDMS:
		boundary.ServiceKind = awscloud.ServiceDMS
	default:
		return nil, fmt.Errorf("dms scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot DMS metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, group := range snapshot.SubnetGroups {
		next, err := subnetGroupEnvelopes(boundary, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, instance := range snapshot.ReplicationInstances {
		next, err := instanceEnvelopes(boundary, instance)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, endpoint := range snapshot.Endpoints {
		next, err := endpointEnvelopes(boundary, endpoint)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, task := range snapshot.Tasks {
		next, err := taskEnvelopes(boundary, task)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func instanceEnvelopes(boundary awscloud.Boundary, instance ReplicationInstance) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(instanceObservation(boundary, instance))
	if err != nil {
		return nil, err
	}
	return appendRelationships([]facts.Envelope{resource}, instanceRelationships(boundary, instance))
}

func subnetGroupEnvelopes(boundary awscloud.Boundary, group ReplicationSubnetGroup) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(subnetGroupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	return appendRelationships([]facts.Envelope{resource}, subnetGroupRelationships(boundary, group))
}

func endpointEnvelopes(boundary awscloud.Boundary, endpoint Endpoint) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(endpointObservation(boundary, endpoint))
	if err != nil {
		return nil, err
	}
	return appendRelationships([]facts.Envelope{resource}, endpointRelationships(boundary, endpoint))
}

func taskEnvelopes(boundary awscloud.Boundary, task ReplicationTask) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(taskObservation(boundary, task))
	if err != nil {
		return nil, err
	}
	return appendRelationships([]facts.Envelope{resource}, taskRelationships(boundary, task))
}

// appendRelationships converts each relationship observation into an envelope
// and appends it to base, so every resource node and its outgoing edges land in
// one slice.
func appendRelationships(
	base []facts.Envelope,
	relationships []awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	for _, observation := range relationships {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		base = append(base, envelope)
	}
	return base, nil
}

func instanceObservation(boundary awscloud.Boundary, instance ReplicationInstance) awscloud.ResourceObservation {
	arn := strings.TrimSpace(instance.ARN)
	identifier := strings.TrimSpace(instance.Identifier)
	resourceID := instanceResourceID(instance)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDMSReplicationInstance,
		Name:         identifier,
		State:        strings.TrimSpace(instance.Status),
		Tags:         cloneStringMap(instance.Tags),
		Attributes: map[string]any{
			"replication_instance_class": strings.TrimSpace(instance.Class),
			"engine_version":             strings.TrimSpace(instance.EngineVersion),
			"allocated_storage_gib":      instance.AllocatedStorageGiB,
			"multi_az":                   instance.MultiAZ,
			"publicly_accessible":        instance.PubliclyAccessible,
			"availability_zone":          strings.TrimSpace(instance.AvailabilityZone),
			"network_type":               strings.TrimSpace(instance.NetworkType),
			"kms_key_id":                 strings.TrimSpace(instance.KMSKeyID),
			"subnet_group_identifier":    strings.TrimSpace(instance.SubnetGroupIdentifier),
			"vpc_id":                     strings.TrimSpace(instance.VPCID),
			"subnet_ids":                 cloneStrings(instance.SubnetIDs),
			"security_group_ids":         cloneStrings(instance.SecurityGroupIDs),
			"create_time":                timeOrNil(instance.CreateTime),
		},
		CorrelationAnchors: []string{arn, identifier},
		SourceRecordID:     resourceID,
	}
}

func subnetGroupObservation(boundary awscloud.Boundary, group ReplicationSubnetGroup) awscloud.ResourceObservation {
	identifier := subnetGroupResourceID(group)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   identifier,
		ResourceType: awscloud.ResourceTypeDMSReplicationSubnetGroup,
		Name:         identifier,
		State:        strings.TrimSpace(group.Status),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"description": strings.TrimSpace(group.Description),
			"vpc_id":      strings.TrimSpace(group.VPCID),
			"subnet_ids":  cloneStrings(group.SubnetIDs),
		},
		CorrelationAnchors: []string{identifier},
		SourceRecordID:     identifier,
	}
}

func endpointObservation(boundary awscloud.Boundary, endpoint Endpoint) awscloud.ResourceObservation {
	arn := strings.TrimSpace(endpoint.ARN)
	identifier := strings.TrimSpace(endpoint.Identifier)
	resourceID := endpointResourceID(endpoint)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDMSEndpoint,
		Name:         identifier,
		State:        strings.TrimSpace(endpoint.Status),
		Attributes: map[string]any{
			"endpoint_type":             strings.TrimSpace(endpoint.EndpointType),
			"engine_name":               strings.TrimSpace(endpoint.EngineName),
			"engine_display_name":       strings.TrimSpace(endpoint.EngineDisplayName),
			"ssl_mode":                  strings.TrimSpace(endpoint.SSLMode),
			"database_name":             strings.TrimSpace(endpoint.DatabaseName),
			"port":                      endpoint.Port,
			"kms_key_id":                strings.TrimSpace(endpoint.KMSKeyID),
			"s3_bucket_name":            strings.TrimSpace(endpoint.S3BucketName),
			"kinesis_stream_arn":        strings.TrimSpace(endpoint.KinesisStreamARN),
			"secrets_manager_secret_id": strings.TrimSpace(endpoint.SecretsManagerSecretID),
			"uses_secrets_manager":      strings.TrimSpace(endpoint.SecretsManagerSecretID) != "",
		},
		CorrelationAnchors: []string{arn, identifier},
		SourceRecordID:     resourceID,
	}
}

func taskObservation(boundary awscloud.Boundary, task ReplicationTask) awscloud.ResourceObservation {
	arn := strings.TrimSpace(task.ARN)
	identifier := strings.TrimSpace(task.Identifier)
	resourceID := taskResourceID(task)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDMSReplicationTask,
		Name:         identifier,
		State:        strings.TrimSpace(task.Status),
		Tags:         cloneStringMap(task.Tags),
		Attributes: map[string]any{
			"migration_type":           strings.TrimSpace(task.MigrationType),
			"source_endpoint_arn":      strings.TrimSpace(task.SourceEndpointARN),
			"target_endpoint_arn":      strings.TrimSpace(task.TargetEndpointARN),
			"replication_instance_arn": strings.TrimSpace(task.ReplicationInstanceARN),
			"creation_date":            timeOrNil(task.CreationDate),
		},
		CorrelationAnchors: []string{arn, identifier},
		SourceRecordID:     resourceID,
	}
}
