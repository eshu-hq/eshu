// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codebuild

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func projectObservation(boundary awscloud.Boundary, project Project) awscloud.ResourceObservation {
	arn := strings.TrimSpace(project.ARN)
	name := strings.TrimSpace(project.Name)
	resourceID := firstNonEmpty(arn, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodeBuildProject,
		Name:         name,
		Tags:         cloneStringMap(project.Tags),
		Attributes: map[string]any{
			"description":            strings.TrimSpace(project.Description),
			"service_role_arn":       strings.TrimSpace(project.ServiceRoleARN),
			"encryption_key_id":      strings.TrimSpace(project.EncryptionKeyID),
			"timeout_in_minutes":     project.TimeoutInMinutes,
			"queued_timeout_minutes": project.QueuedTimeout,
			"concurrent_build_limit": project.ConcurrentBuilds,
			"created":                timeOrNil(project.Created),
			"last_modified":          timeOrNil(project.LastModified),
			"source":                 sourceAttribute(project.Source),
			"secondary_sources":      sourceAttributes(project.SecondarySources),
			"environment":            environmentAttribute(project.Environment),
			"artifacts":              artifactsAttribute(project.Artifacts),
			"secondary_artifacts":    artifactsAttributes(project.SecondaryArtifacts),
			"vpc_config":             vpcConfigAttribute(project.VPCConfig),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func reportGroupObservation(boundary awscloud.Boundary, group ReportGroup) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := firstNonEmpty(arn, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodeBuildReportGroup,
		Name:         name,
		State:        strings.TrimSpace(group.Status),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"type":             strings.TrimSpace(group.Type),
			"status":           strings.TrimSpace(group.Status),
			"export_type":      strings.TrimSpace(group.ExportType),
			"export_s3_bucket": strings.TrimSpace(group.ExportS3Bucket),
			"created":          timeOrNil(group.Created),
			"last_modified":    timeOrNil(group.LastModified),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func buildObservation(boundary awscloud.Boundary, build Build) awscloud.ResourceObservation {
	arn := strings.TrimSpace(build.ARN)
	id := strings.TrimSpace(build.ID)
	resourceID := firstNonEmpty(arn, id)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodeBuildBuild,
		Name:         id,
		State:        strings.TrimSpace(build.Status),
		Attributes: map[string]any{
			"build_id":                id,
			"project_name":            strings.TrimSpace(build.ProjectName),
			"build_number":            build.BuildNumber,
			"status":                  strings.TrimSpace(build.Status),
			"current_phase":           strings.TrimSpace(build.CurrentPhase),
			"initiator":               strings.TrimSpace(build.Initiator),
			"build_complete":          build.BuildComplete,
			"resolved_source_version": strings.TrimSpace(build.ResolvedSourceVersion),
			"start_time":              timeOrNil(build.StartTime),
			"end_time":                timeOrNil(build.EndTime),
			"duration_seconds":        durationSeconds(build.StartTime, build.EndTime),
		},
		CorrelationAnchors: []string{arn, id},
		SourceRecordID:     resourceID,
	}
}

func sourceAttribute(source ProjectSource) map[string]any {
	return map[string]any{
		"type":                strings.TrimSpace(source.Type),
		"location":            strings.TrimSpace(source.Location),
		"source_identifier":   strings.TrimSpace(source.SourceIdentifier),
		"report_build_status": source.ReportBuildStatus,
	}
}

func sourceAttributes(sources []ProjectSource) []map[string]any {
	if len(sources) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		out = append(out, sourceAttribute(source))
	}
	return out
}

// environmentAttribute maps the build environment into a fact payload. The
// environment-variable entries carry name and type only; PLAINTEXT values are
// the redaction marker map and reference values name the parameter or secret.
// The raw PLAINTEXT value and buildspec body never reach this function.
func environmentAttribute(environment ProjectEnvironment) map[string]any {
	return map[string]any{
		"type":                   strings.TrimSpace(environment.Type),
		"image":                  strings.TrimSpace(environment.Image),
		"compute_type":           strings.TrimSpace(environment.ComputeType),
		"privileged_mode":        environment.PrivilegedMode,
		"image_pull_credentials": strings.TrimSpace(environment.ImagePullCredentials),
		"environment_variables":  environmentVariableAttributes(environment.EnvironmentVariables),
	}
}

func environmentVariableAttributes(variables []EnvironmentVariable) []map[string]any {
	if len(variables) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(variables))
	for _, variable := range variables {
		entry := map[string]any{
			"name": strings.TrimSpace(variable.Name),
			"type": strings.TrimSpace(variable.Type),
		}
		if reference := strings.TrimSpace(variable.Reference); reference != "" {
			entry["reference"] = reference
		}
		if len(variable.ValueMarker) > 0 {
			entry["value"] = cloneAnyMap(variable.ValueMarker)
		}
		out = append(out, entry)
	}
	return out
}

func artifactsAttribute(artifacts ProjectArtifacts) map[string]any {
	return map[string]any{
		"type":                strings.TrimSpace(artifacts.Type),
		"location":            strings.TrimSpace(artifacts.Location),
		"artifact_identifier": strings.TrimSpace(artifacts.ArtifactIdentifier),
		"encryption_disabled": artifacts.EncryptionDisabled,
	}
}

func artifactsAttributes(artifacts []ProjectArtifacts) []map[string]any {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, artifactsAttribute(artifact))
	}
	return out
}

func vpcConfigAttribute(config VPCConfig) map[string]any {
	return map[string]any{
		"vpc_id":             strings.TrimSpace(config.VPCID),
		"subnet_ids":         cloneStrings(config.SubnetIDs),
		"security_group_ids": cloneStrings(config.SecurityGroupIDs),
	}
}

// durationSeconds reports the wall-clock build duration in seconds when both
// timestamps are present and ordered. It returns nil otherwise so an in-flight
// build records no duration rather than a misleading value.
func durationSeconds(start, end time.Time) any {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return nil
	}
	return int64(end.Sub(start).Seconds())
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
