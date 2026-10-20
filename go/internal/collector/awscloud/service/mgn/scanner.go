// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mgn

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Application Migration Service (MGN) metadata-only facts for
// one claimed account and region. It never reads or persists replication-agent
// credentials, replication configuration secrets, or replicated disk contents,
// and never mutates MGN state. It reports applications, source servers, launch
// configurations, and jobs plus the application-contains-source-server,
// source-server-launched-EC2-instance, launch-config-uses-launch-template, and
// job-targets-source-server relationships.
type Scanner struct {
	// Client is the metadata-only MGN snapshot source.
	Client Client
}

// Scan observes MGN applications, source servers, launch configurations, jobs,
// and their direct cross-service and internal dependency metadata through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("mgn scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceMGN:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceMGN
	default:
		return nil, fmt.Errorf("mgn scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot MGN metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, application := range snapshot.Applications {
		next, err := applicationEnvelopes(boundary, application)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, server := range snapshot.SourceServers {
		next, err := sourceServerEnvelopes(boundary, server)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, job := range snapshot.Jobs {
		next, err := jobEnvelopes(boundary, job)
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

func applicationEnvelopes(boundary awscloud.Boundary, application Application) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(applicationObservation(boundary, application))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

func sourceServerEnvelopes(boundary awscloud.Boundary, server SourceServer) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(sourceServerObservation(boundary, server))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if server.LaunchConfiguration != nil {
		launchConfig, err := awscloud.NewResourceEnvelope(launchConfigurationObservation(boundary, server))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, launchConfig)
	}
	for _, relationship := range []*awscloud.RelationshipObservation{
		applicationContainsSourceServerRelationship(boundary, server),
		sourceServerLaunchedEC2Relationship(boundary, server),
		launchConfigurationUsesLaunchTemplateRelationship(boundary, server),
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

func jobEnvelopes(boundary awscloud.Boundary, job Job) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(jobObservation(boundary, job))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range jobTargetsSourceServerRelationships(boundary, job) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func applicationObservation(boundary awscloud.Boundary, application Application) awscloud.ResourceObservation {
	arn := strings.TrimSpace(application.ARN)
	name := strings.TrimSpace(application.Name)
	resourceID := applicationResourceID(application)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMGNApplication,
		Name:         name,
		Tags:         cloneStringMap(application.Tags),
		Attributes: map[string]any{
			"application_id":       strings.TrimSpace(application.ApplicationID),
			"description":          strings.TrimSpace(application.Description),
			"wave_id":              strings.TrimSpace(application.WaveID),
			"is_archived":          application.IsArchived,
			"health_status":        strings.TrimSpace(application.HealthStatus),
			"progress_status":      strings.TrimSpace(application.ProgressStatus),
			"total_source_servers": application.TotalSourceServers,
			"creation_time":        timeOrNil(application.CreationTime),
			"last_modified_time":   timeOrNil(application.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}

func sourceServerObservation(boundary awscloud.Boundary, server SourceServer) awscloud.ResourceObservation {
	arn := strings.TrimSpace(server.ARN)
	resourceID := sourceServerResourceID(server)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMGNSourceServer,
		Name:         strings.TrimSpace(server.Hostname),
		State:        strings.TrimSpace(server.LifeCycleState),
		Tags:         cloneStringMap(server.Tags),
		Attributes: map[string]any{
			"source_server_id":          strings.TrimSpace(server.SourceServerID),
			"application_id":            strings.TrimSpace(server.ApplicationID),
			"data_replication_state":    strings.TrimSpace(server.DataReplicationState),
			"replication_type":          strings.TrimSpace(server.ReplicationType),
			"is_archived":               server.IsArchived,
			"recommended_instance_type": strings.TrimSpace(server.RecommendedInstanceType),
			"os":                        strings.TrimSpace(server.OS),
			"hostname":                  strings.TrimSpace(server.Hostname),
			"fqdn":                      strings.TrimSpace(server.FQDN),
			"aws_instance_id":           strings.TrimSpace(server.AWSInstanceID),
			"launched_ec2_instance_id":  strings.TrimSpace(server.LaunchedEC2InstanceID),
			"vcenter_client_id":         strings.TrimSpace(server.VcenterClientID),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}

func launchConfigurationObservation(boundary awscloud.Boundary, server SourceServer) awscloud.ResourceObservation {
	config := server.LaunchConfiguration
	resourceID := launchConfigurationResourceID(server.SourceServerID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMGNLaunchConfiguration,
		Name:         strings.TrimSpace(config.Name),
		Tags:         nil,
		Attributes: map[string]any{
			"source_server_id":   strings.TrimSpace(server.SourceServerID),
			"launch_disposition": strings.TrimSpace(config.LaunchDisposition),
			"boot_mode":          strings.TrimSpace(config.BootMode),
			"target_instance_type_right_sizing_method": strings.TrimSpace(config.TargetInstanceTypeRightSizingMethod),
			"ec2_launch_template_id":                   strings.TrimSpace(config.EC2LaunchTemplateID),
			"copy_private_ip":                          config.CopyPrivateIP,
			"copy_tags":                                config.CopyTags,
		},
		CorrelationAnchors: []string{resourceID},
		SourceRecordID:     resourceID,
	}
}

func jobObservation(boundary awscloud.Boundary, job Job) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	resourceID := jobResourceID(job)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMGNJob,
		State:        strings.TrimSpace(job.Status),
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"job_id":                          strings.TrimSpace(job.JobID),
			"type":                            strings.TrimSpace(job.Type),
			"status":                          strings.TrimSpace(job.Status),
			"initiated_by":                    strings.TrimSpace(job.InitiatedBy),
			"participating_source_server_ids": cloneStrings(job.ParticipatingSourceServerIDs),
			"creation_time":                   timeOrNil(job.CreationTime),
			"end_time":                        timeOrNil(job.EndTime),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}
