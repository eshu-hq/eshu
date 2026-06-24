// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS DataSync metadata-only facts for one claimed account and
// region. It never starts, cancels, creates, updates, or deletes a transfer
// task, location, or agent, and never reads the object or record contents the
// task moves, access keys, server certificates, or storage credentials.
type Scanner struct {
	Client Client
}

// Scan observes DataSync transfer tasks, transfer locations, and agents through
// the configured metadata-only client, then emits resource facts plus
// relationship evidence for task-to-source/destination-location,
// task-to-CloudWatch-log-group, location-to-S3/EFS/FSx storage, and
// location-to-IAM-role joins.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("datasync scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDataSync:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDataSync
	default:
		return nil, fmt.Errorf("datasync scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	tasks, err := s.Client.ListTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DataSync tasks: %w", err)
	}
	for _, task := range tasks {
		next, err := taskEnvelopes(boundary, task)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	locations, err := s.Client.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DataSync locations: %w", err)
	}
	for _, location := range locations {
		next, err := locationEnvelopes(boundary, location)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	agents, err := s.Client.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DataSync agents: %w", err)
	}
	for _, agent := range agents {
		envelope, err := awscloud.NewResourceEnvelope(agentObservation(boundary, agent))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func taskEnvelopes(boundary awscloud.Boundary, task Task) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(taskObservation(boundary, task))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		taskSourceLocationRelationship(boundary, task),
		taskDestinationLocationRelationship(boundary, task),
		taskLogGroupRelationship(boundary, task),
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

func locationEnvelopes(boundary awscloud.Boundary, location Location) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(locationObservation(boundary, location))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		locationS3Relationship(boundary, location),
		locationEFSRelationship(boundary, location),
		locationFSxRelationship(boundary, location),
		locationRoleRelationship(boundary, location),
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

func taskObservation(boundary awscloud.Boundary, task Task) awscloud.ResourceObservation {
	arn := strings.TrimSpace(task.ARN)
	name := strings.TrimSpace(task.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeDataSyncTask,
		Name:         name,
		State:        strings.TrimSpace(task.Status),
		Attributes: map[string]any{
			"source_location_arn":      strings.TrimSpace(task.SourceLocationARN),
			"destination_location_arn": strings.TrimSpace(task.DestinationLocationARN),
			"cloudwatch_log_group_arn": trimLogGroupWildcardARN(task.CloudWatchLogGroupARN),
			"schedule_expression":      strings.TrimSpace(task.ScheduleExpression),
			"schedule_status":          strings.TrimSpace(task.ScheduleStatus),
			"task_mode":                strings.TrimSpace(task.TaskMode),
			"creation_time":            timeOrNil(task.CreationTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

func locationObservation(boundary awscloud.Boundary, location Location) awscloud.ResourceObservation {
	arn := strings.TrimSpace(location.ARN)
	uri := strings.TrimSpace(location.URI)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeDataSyncLocation,
		Name:         uri,
		Attributes: map[string]any{
			"location_type":      strings.TrimSpace(location.Type),
			"location_uri":       uri,
			"s3_bucket_name":     strings.TrimSpace(location.S3BucketName),
			"efs_file_system_id": strings.TrimSpace(location.EFSFileSystemID),
			"fsx_file_system_id": strings.TrimSpace(location.FSxFileSystemID),
			"iam_role_arn":       strings.TrimSpace(location.IAMRoleARN),
			"creation_time":      timeOrNil(location.CreationTime),
		},
		CorrelationAnchors: []string{arn, uri},
		SourceRecordID:     arn,
	}
}

func agentObservation(boundary awscloud.Boundary, agent Agent) awscloud.ResourceObservation {
	arn := strings.TrimSpace(agent.ARN)
	name := strings.TrimSpace(agent.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeDataSyncAgent,
		Name:         name,
		State:        strings.TrimSpace(agent.Status),
		Attributes: map[string]any{
			"endpoint_type":    strings.TrimSpace(agent.EndpointType),
			"platform_version": strings.TrimSpace(agent.PlatformVersion),
			"creation_time":    timeOrNil(agent.CreationTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}
