// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mwaa

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon MWAA metadata-only facts for one claimed account and
// region. It never creates, updates, or deletes an environment, never requests
// an Airflow CLI token or web-login token, and never reads or persists Apache
// Airflow configuration option values, connection strings, or secrets.
type Scanner struct {
	Client Client
}

// Scan observes Amazon MWAA environments through the configured client and
// emits one resource per environment plus relationship edges to the S3 DAG
// bucket, VPC subnets and security groups, the IAM execution role, the KMS
// key, and the CloudWatch Logs log groups the environment publishes Airflow
// logs to. Apache Airflow configuration option values stay outside the
// contract.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("mwaa scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceMWAA:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceMWAA
	default:
		return nil, fmt.Errorf("mwaa scanner received service_kind %q", boundary.ServiceKind)
	}

	environments, err := s.Client.ListEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MWAA environments: %w", err)
	}

	var envelopes []facts.Envelope
	for _, environment := range environments {
		next, err := environmentEnvelopes(boundary, environment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func environmentEnvelopes(boundary awscloud.Boundary, environment Environment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(environmentObservation(boundary, environment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range environmentRelationships(boundary, environment) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func environmentObservation(boundary awscloud.Boundary, environment Environment) awscloud.ResourceObservation {
	arn := strings.TrimSpace(environment.ARN)
	name := strings.TrimSpace(environment.Name)
	resourceID := environmentResourceID(environment)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMWAAEnvironment,
		Name:         name,
		State:        strings.TrimSpace(environment.Status),
		Tags:         cloneStringMap(environment.Tags),
		Attributes: map[string]any{
			"airflow_version":       strings.TrimSpace(environment.AirflowVersion),
			"webserver_access_mode": strings.TrimSpace(environment.WebserverAccessMode),
			"environment_class":     strings.TrimSpace(environment.EnvironmentClass),
			"endpoint_management":   strings.TrimSpace(environment.EndpointManagement),
			"schedulers":            environment.Schedulers,
			"min_workers":           environment.MinWorkers,
			"max_workers":           environment.MaxWorkers,
			"min_webservers":        environment.MinWebservers,
			"max_webservers":        environment.MaxWebservers,
			"created_at":            timeOrNil(environment.CreatedAt),
			"source_bucket_arn":     strings.TrimSpace(environment.SourceBucketARN),
			"execution_role_arn":    strings.TrimSpace(environment.ExecutionRoleARN),
			"service_role_arn":      strings.TrimSpace(environment.ServiceRoleARN),
			"kms_key":               strings.TrimSpace(environment.KMSKey),
			"subnet_ids":            cloneStringSlice(environment.SubnetIDs),
			"security_group_ids":    cloneStringSlice(environment.SecurityGroupIDs),
			"log_module_count":      len(environment.LogGroups),
		},
		CorrelationAnchors: correlationAnchors(arn, name),
		SourceRecordID:     resourceID,
	}
}

// correlationAnchors returns the distinct, non-empty identity anchors for an
// environment so downstream correlation can join on either the ARN or the
// bare environment name.
func correlationAnchors(arn, name string) []string {
	anchors := make([]string, 0, 2)
	for _, candidate := range []string{arn, name} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			anchors = append(anchors, trimmed)
		}
	}
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}
