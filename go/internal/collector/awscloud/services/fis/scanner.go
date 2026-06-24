// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fis

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Fault Injection Service (FIS) metadata-only facts for one
// claimed account and region. It never starts, stops, or mutates an experiment,
// and never reads experiment run results or resolved-target inventories. It
// reports experiment templates plus the template-to-IAM-role,
// template-to-target (EC2 instance, ECS cluster, RDS instance/cluster),
// template-to-CloudWatch-log-group, template-to-S3, and template-to-CloudWatch-
// alarm (stop condition) relationships.
type Scanner struct {
	// Client is the metadata-only FIS snapshot source.
	Client Client
}

// Scan observes FIS experiment templates and their direct IAM, target,
// logging, and stop-condition dependency metadata through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("fis scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceFIS:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceFIS
	default:
		return nil, fmt.Errorf("fis scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list FIS experiment templates: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, template := range snapshot.Templates {
		next, err := templateEnvelopes(boundary, template)
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

func templateEnvelopes(boundary awscloud.Boundary, template ExperimentTemplate) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(templateObservation(boundary, template))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range templateRelationships(boundary, template) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func templateObservation(boundary awscloud.Boundary, template ExperimentTemplate) awscloud.ResourceObservation {
	templateARN := strings.TrimSpace(template.ARN)
	resourceID := templateResourceID(template)
	name := firstNonEmpty(template.Name, template.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          templateARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeFISExperimentTemplate,
		Name:         name,
		Tags:         cloneStringMap(template.Tags),
		Attributes: map[string]any{
			"template_id":           strings.TrimSpace(template.ID),
			"description":           strings.TrimSpace(template.Description),
			"role_arn":              strings.TrimSpace(template.RoleARN),
			"action_ids":            actionIDs(template.Actions),
			"action_count":          len(template.Actions),
			"target_keys":           targetKeys(template.Targets),
			"target_resource_types": targetResourceTypes(template.Targets),
			"target_count":          len(template.Targets),
			"log_group_arn":         strings.TrimSpace(template.LogGroupARN),
			"log_s3_bucket":         strings.TrimSpace(template.LogS3Bucket),
			"stop_condition_count":  len(template.StopConditionAlarmARNs),
			"cloudwatch_logging":    strings.TrimSpace(template.LogGroupARN) != "",
			"s3_logging":            strings.TrimSpace(template.LogS3Bucket) != "",
			"creation_time":         timeOrNil(template.CreationTime),
			"last_update_time":      timeOrNil(template.LastUpdateTime),
		},
		CorrelationAnchors: []string{templateARN, strings.TrimSpace(template.ID), name},
		SourceRecordID:     resourceID,
	}
}

// actionIDs returns the de-duplicated FIS action identifiers the template runs,
// keeping only the action id (for example aws:ec2:stop-instances); parameter
// values are never included.
func actionIDs(actions []Action) []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, action := range actions {
		id := strings.TrimSpace(action.ActionID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// targetKeys returns the template's target keys for the resource attribute
// summary.
func targetKeys(targets []Target) []string {
	var keys []string
	for _, target := range targets {
		if key := strings.TrimSpace(target.Key); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// targetResourceTypes returns the de-duplicated FIS resource-type selectors the
// template targets (for example aws:ec2:instance), for the resource attribute
// summary.
func targetResourceTypes(targets []Target) []string {
	seen := make(map[string]struct{})
	var types []string
	for _, target := range targets {
		resourceType := strings.TrimSpace(target.ResourceType)
		if resourceType == "" {
			continue
		}
		if _, exists := seen[resourceType]; exists {
			continue
		}
		seen[resourceType] = struct{}{}
		types = append(types, resourceType)
	}
	return types
}
