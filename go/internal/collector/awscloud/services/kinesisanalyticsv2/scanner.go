// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesisanalyticsv2

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Managed Service for Apache Flink (Kinesis Data Analytics
// v2) metadata-only facts for one claimed account and region. It reads
// application control-plane metadata through the management APIs and never reads
// or persists application code bodies, SQL text, environment property values,
// run-configuration content, or record payloads, and never mutates application
// state.
type Scanner struct {
	// Client is the metadata-only Managed Flink read surface. It is required.
	Client Client
}

// Scan observes Managed Flink applications through the configured client and
// emits one resource fact per application plus relationship facts for the
// application's SQL input/output Kinesis data streams and Firehose delivery
// streams, its S3 code bucket, its VPC subnets and security groups, its service
// execution IAM role, and its CloudWatch logging log groups.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("kinesisanalyticsv2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceKinesisAnalyticsV2:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceKinesisAnalyticsV2
	default:
		return nil, fmt.Errorf("kinesisanalyticsv2 scanner received service_kind %q", boundary.ServiceKind)
	}

	applications, err := s.Client.ListApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Managed Flink applications: %w", err)
	}

	var envelopes []facts.Envelope
	for _, application := range applications {
		resource, err := awscloud.NewResourceEnvelope(applicationObservation(boundary, application))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		relationships, err := relationshipEnvelopes(applicationRelationships(boundary, application))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

// relationshipEnvelopes wraps each relationship observation in a fact envelope.
// It returns a nil slice for an empty input so callers append nothing.
func relationshipEnvelopes(observations []awscloud.RelationshipObservation) ([]facts.Envelope, error) {
	if len(observations) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(observations))
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// applicationObservation maps one application's metadata into a resource
// observation. It records identity, runtime, mode, parallelism, posture,
// version counters, and lifecycle timestamps only. Application code bodies, SQL
// text, environment property values, and run-configuration content stay outside
// the contract.
func applicationObservation(boundary awscloud.Boundary, application Application) awscloud.ResourceObservation {
	arn := strings.TrimSpace(application.ARN)
	name := strings.TrimSpace(application.Name)
	resourceID := applicationResourceID(application)
	attributes := map[string]any{
		"application_name":      name,
		"runtime_environment":   strings.TrimSpace(application.RuntimeEnvironment),
		"application_mode":      strings.TrimSpace(application.Mode),
		"version_id":            application.VersionID,
		"version_count":         application.VersionCount,
		"snapshots_enabled":     application.SnapshotsEnabled,
		"auto_scaling_enabled":  application.AutoScalingEnabled,
		"create_timestamp":      timeOrNil(application.CreateTimestamp),
		"last_update_timestamp": timeOrNil(application.LastUpdateTimestamp),
	}
	if description := strings.TrimSpace(application.Description); description != "" {
		attributes["description"] = description
	}
	if role := strings.TrimSpace(application.ServiceExecutionRoleARN); role != "" {
		attributes["service_execution_role_arn"] = role
	}
	if parallelismType := strings.TrimSpace(application.ParallelismConfigurationType); parallelismType != "" {
		attributes["parallelism_configuration_type"] = parallelismType
		attributes["parallelism"] = application.Parallelism
		attributes["parallelism_per_kpu"] = application.ParallelismPerKPU
		attributes["current_parallelism"] = application.CurrentParallelism
	}
	if codeContentType := strings.TrimSpace(application.CodeContentType); codeContentType != "" {
		attributes["code_content_type"] = codeContentType
	}
	if codeBucket := strings.TrimSpace(application.CodeS3BucketARN); codeBucket != "" {
		attributes["code_s3_bucket_arn"] = codeBucket
	}
	if codeKey := strings.TrimSpace(application.CodeS3FileKey); codeKey != "" {
		attributes["code_s3_file_key"] = codeKey
	}
	if snapshotNames := snapshotNames(application.Snapshots); len(snapshotNames) > 0 {
		attributes["snapshot_count"] = len(snapshotNames)
		attributes["snapshot_names"] = snapshotNames
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeManagedFlinkApplication,
		Name:               name,
		State:              strings.TrimSpace(application.Status),
		Tags:               cloneStringMap(application.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
