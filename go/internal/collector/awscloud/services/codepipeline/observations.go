// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codepipeline

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// pipelineARN builds the canonical CodePipeline pipeline ARN. ListPipelines
// does not return ARNs, so the scanner derives a stable identity from the
// boundary account and region plus the pipeline name, matching the documented
// AWS ARN format. The partition is derived from the boundary, never hardcoded.
func pipelineARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codepipeline:%s:%s:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

func pipelineObservation(boundary awscloud.Boundary, pipeline Pipeline) awscloud.ResourceObservation {
	arn := firstNonEmpty(pipeline.ARN, pipelineARN(boundary, pipeline.Name))
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, pipeline.Name),
		ResourceType: awscloud.ResourceTypeCodePipelinePipeline,
		Name:         strings.TrimSpace(pipeline.Name),
		Tags:         cloneStringMap(pipeline.Tags),
		Attributes: map[string]any{
			"role_arn":       strings.TrimSpace(pipeline.RoleARN),
			"pipeline_type":  strings.TrimSpace(pipeline.PipelineType),
			"execution_mode": strings.TrimSpace(pipeline.ExecutionMode),
			"version":        pipeline.Version,
			"created":        timeOrNil(pipeline.Created),
			"updated":        timeOrNil(pipeline.Updated),
			"stage_count":    len(pipeline.Stages),
			"action_count":   actionCount(pipeline),
			"artifact_store": artifactStoreAttribute(pipeline.ArtifactStore),
			"stages":         stageAttributes(pipeline.Stages),
		},
		CorrelationAnchors: []string{arn, pipeline.Name},
		SourceRecordID:     firstNonEmpty(arn, pipeline.Name),
	}
}

func executionObservation(
	boundary awscloud.Boundary,
	pipeline Pipeline,
	execution Execution,
) awscloud.ResourceObservation {
	pipelineArnValue := firstNonEmpty(pipeline.ARN, pipelineARN(boundary, pipeline.Name))
	resourceID := executionResourceID(pipeline.Name, execution.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodePipelineExecution,
		Name:         strings.TrimSpace(execution.ID),
		State:        strings.TrimSpace(execution.Status),
		Attributes: map[string]any{
			"pipeline_name":    strings.TrimSpace(pipeline.Name),
			"pipeline_arn":     pipelineArnValue,
			"execution_id":     strings.TrimSpace(execution.ID),
			"status":           strings.TrimSpace(execution.Status),
			"execution_mode":   strings.TrimSpace(execution.ExecutionMode),
			"execution_type":   strings.TrimSpace(execution.ExecutionType),
			"trigger_type":     strings.TrimSpace(execution.TriggerType),
			"start_time":       timeOrNil(execution.StartTime),
			"last_update_time": timeOrNil(execution.LastUpdateTime),
			"source_revisions": sourceRevisionAttributes(execution.SourceRevisions),
		},
		CorrelationAnchors: []string{resourceID, pipelineArnValue},
		SourceRecordID:     resourceID,
	}
}

func webhookObservation(boundary awscloud.Boundary, webhook Webhook) awscloud.ResourceObservation {
	arn := strings.TrimSpace(webhook.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, webhook.Name),
		ResourceType: awscloud.ResourceTypeCodePipelineWebhook,
		Name:         strings.TrimSpace(webhook.Name),
		Tags:         cloneStringMap(webhook.Tags),
		Attributes: map[string]any{
			"target_pipeline":     strings.TrimSpace(webhook.TargetPipeline),
			"target_action":       strings.TrimSpace(webhook.TargetAction),
			"authentication_type": strings.TrimSpace(webhook.AuthenticationType),
			"last_triggered":      timeOrNil(webhook.LastTriggered),
		},
		CorrelationAnchors: []string{arn, webhook.Name},
		SourceRecordID:     firstNonEmpty(arn, webhook.Name),
	}
}

func actionTypeObservation(boundary awscloud.Boundary, actionType ActionType) awscloud.ResourceObservation {
	resourceID := actionTypeResourceID(actionType)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodePipelineActionType,
		Name:         resourceID,
		Attributes: map[string]any{
			"category":                    strings.TrimSpace(actionType.Category),
			"owner":                       strings.TrimSpace(actionType.Owner),
			"provider":                    strings.TrimSpace(actionType.Provider),
			"version":                     strings.TrimSpace(actionType.Version),
			"configuration_property_keys": cloneStrings(actionType.ConfigurationPropertyKeys),
		},
		CorrelationAnchors: []string{resourceID},
		SourceRecordID:     resourceID,
	}
}

func artifactStoreAttribute(store ArtifactStoreSummary) map[string]any {
	return map[string]any{
		"type":         strings.TrimSpace(store.Type),
		"s3_bucket":    strings.TrimSpace(store.S3Bucket),
		"kms_key_id":   strings.TrimSpace(store.KMSKeyID),
		"kms_key_type": strings.TrimSpace(store.KMSKeyType),
		"regions":      cloneStrings(store.Regions),
	}
}

// stageAttributes maps stage and action structure into fact payload maps. It
// emits action configuration KEY names only; no action configuration value is
// ever present because the scanner-owned Action carries no value field.
func stageAttributes(stages []Stage) []map[string]any {
	if len(stages) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(stages))
	for _, stage := range stages {
		actions := make([]map[string]any, 0, len(stage.Actions))
		for _, action := range stage.Actions {
			actions = append(actions, map[string]any{
				"name":               action.Name,
				"category":           action.Category,
				"owner":              action.Owner,
				"provider":           action.Provider,
				"version":            action.Version,
				"run_order":          action.RunOrder,
				"region":             strings.TrimSpace(action.Region),
				"role_arn":           strings.TrimSpace(action.RoleARN),
				"configuration_keys": cloneStrings(action.ConfigurationKeys),
				"source_provider":    strings.TrimSpace(action.SourceProvider),
				"target_provider":    strings.TrimSpace(action.TargetProvider),
				"target_resource":    strings.TrimSpace(action.TargetResourceName),
			})
		}
		out = append(out, map[string]any{
			"name":    stage.Name,
			"actions": actions,
		})
	}
	return out
}

func sourceRevisionAttributes(revisions []SourceRevision) []map[string]any {
	if len(revisions) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(revisions))
	for _, revision := range revisions {
		entry := map[string]any{
			"action_name":  strings.TrimSpace(revision.ActionName),
			"revision_id":  strings.TrimSpace(revision.RevisionID),
			"revision_url": strings.TrimSpace(revision.RevisionURL),
			"has_summary":  revision.HasSummary,
		}
		if len(revision.SummaryMarker) > 0 {
			entry["summary"] = cloneAnyMap(revision.SummaryMarker)
		}
		out = append(out, entry)
	}
	return out
}

func actionCount(pipeline Pipeline) int {
	total := 0
	for _, stage := range pipeline.Stages {
		total += len(stage.Actions)
	}
	return total
}

func executionResourceID(pipelineName, executionID string) string {
	pipelineName = strings.TrimSpace(pipelineName)
	executionID = strings.TrimSpace(executionID)
	if pipelineName == "" || executionID == "" {
		return firstNonEmpty(executionID, pipelineName)
	}
	return pipelineName + "#execution#" + executionID
}

func actionTypeResourceID(actionType ActionType) string {
	parts := []string{
		strings.TrimSpace(actionType.Category),
		strings.TrimSpace(actionType.Owner),
		strings.TrimSpace(actionType.Provider),
		strings.TrimSpace(actionType.Version),
	}
	return strings.Join(parts, "/")
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
