// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drs

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Elastic Disaster Recovery metadata-only facts for one
// claimed account and region. It never installs or reads replication agent
// secrets, never reads replicated disk data or point-in-time snapshot contents,
// and never starts, stops, recovers, or mutates DRS state. It reports source
// servers, recovery instances, and replication configuration templates plus the
// source-server-to-recovery-instance and recovery-instance-to-EC2-instance
// relationships.
type Scanner struct {
	// Client is the metadata-only DRS snapshot source.
	Client Client
}

// Scan observes DRS source servers, recovery instances, replication
// configuration templates, and the direct recovery-instance and EC2-instance
// dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("drs scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceDRS:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceDRS
	default:
		return nil, fmt.Errorf("drs scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot DRS metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, server := range snapshot.SourceServers {
		next, err := sourceServerEnvelopes(boundary, server)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, instance := range snapshot.RecoveryInstances {
		next, err := recoveryInstanceEnvelopes(boundary, instance)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, template := range snapshot.ReplicationConfigurationTemplates {
		envelope, err := aws.NewResourceEnvelope(templateObservation(boundary, template))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func sourceServerEnvelopes(boundary aws.Boundary, server SourceServer) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(sourceServerObservation(boundary, server))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := sourceServerRecoversToInstanceRelationship(boundary, server); relationship != nil {
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func recoveryInstanceEnvelopes(boundary aws.Boundary, instance RecoveryInstance) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(recoveryInstanceObservation(boundary, instance))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := recoveryInstanceRunsOnEC2InstanceRelationship(boundary, instance); relationship != nil {
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func sourceServerObservation(boundary aws.Boundary, server SourceServer) aws.ResourceObservation {
	arn := strings.TrimSpace(server.ARN)
	resourceID := sourceServerResourceID(server)
	name := firstNonEmpty(server.Hostname, server.FQDN, resourceID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeDRSSourceServer,
		Name:         name,
		State:        strings.TrimSpace(server.DataReplicationState),
		Tags:         cloneStringMap(server.Tags),
		Attributes: map[string]any{
			"source_server_id":          strings.TrimSpace(server.SourceServerID),
			"hostname":                  stringOrNil(server.Hostname),
			"fqdn":                      stringOrNil(server.FQDN),
			"operating_system":          stringOrNil(server.OperatingSystem),
			"recovery_instance_id":      stringOrNil(server.RecoveryInstanceID),
			"data_replication_state":    stringOrNil(server.DataReplicationState),
			"replication_direction":     stringOrNil(server.ReplicationDirection),
			"last_launch_result":        stringOrNil(server.LastLaunchResult),
			"recommended_instance_type": stringOrNil(server.RecommendedInstanceType),
			"origin_account_id":         stringOrNil(server.OriginAccountID),
			"origin_region":             stringOrNil(server.OriginRegion),
			"origin_availability_zone":  stringOrNil(server.OriginAvailabilityZone),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}

func recoveryInstanceObservation(boundary aws.Boundary, instance RecoveryInstance) aws.ResourceObservation {
	arn := strings.TrimSpace(instance.ARN)
	resourceID := recoveryInstanceResourceID(instance)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeDRSRecoveryInstance,
		Name:         resourceID,
		State:        strings.TrimSpace(instance.EC2InstanceState),
		Tags:         cloneStringMap(instance.Tags),
		Attributes: map[string]any{
			"recovery_instance_id": strings.TrimSpace(instance.RecoveryInstanceID),
			"ec2_instance_id":      stringOrNil(instance.EC2InstanceID),
			"ec2_instance_state":   stringOrNil(instance.EC2InstanceState),
			"source_server_id":     stringOrNil(instance.SourceServerID),
			"is_drill":             instance.IsDrill,
			"origin_environment":   stringOrNil(instance.OriginEnvironment),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}

func templateObservation(boundary aws.Boundary, template ReplicationConfigurationTemplate) aws.ResourceObservation {
	arn := strings.TrimSpace(template.ARN)
	resourceID := templateResourceID(template)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeDRSReplicationConfigurationTemplate,
		Name:         resourceID,
		Tags:         cloneStringMap(template.Tags),
		Attributes: map[string]any{
			"template_id":                      strings.TrimSpace(template.TemplateID),
			"ebs_encryption":                   stringOrNil(template.EBSEncryption),
			"staging_area_subnet_id":           stringOrNil(template.StagingAreaSubnetID),
			"replication_server_instance_type": stringOrNil(template.ReplicationServerInstanceType),
			"use_dedicated_replication_server": template.UseDedicatedReplicationServer,
			"associate_default_security_group": template.AssociateDefaultSecurityGroup,
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}
