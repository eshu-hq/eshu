// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// appObservation builds the aws_resource observation for one Resilience Hub
// application. It records identity, status and drift/compliance labels, the
// configured RPO/RTO targets, the assessment schedule, the resiliency score, and
// the integrated AppRegistry application ARN only.
func appObservation(boundary awscloud.Boundary, app App) awscloud.ResourceObservation {
	arn := strings.TrimSpace(app.ARN)
	name := strings.TrimSpace(app.Name)
	resourceID := appResourceID(app)
	attributes := map[string]any{
		"compliance_status":   strings.TrimSpace(app.ComplianceStatus),
		"drift_status":        strings.TrimSpace(app.DriftStatus),
		"assessment_schedule": strings.TrimSpace(app.AssessmentSchedule),
		"policy_arn":          strings.TrimSpace(app.PolicyARN),
		"resiliency_score":    app.ResiliencyScore,
		"rpo_in_secs":         int32OrNil(app.RPOInSecs),
		"rto_in_secs":         int32OrNil(app.RTOInSecs),
		"creation_time":       timeOrNil(app.CreationTime),
		"input_source_count":  len(app.InputSources),
		"component_count":     len(app.Components),
		"assessment_count":    len(app.Assessments),
	}
	if description := strings.TrimSpace(app.Description); description != "" {
		attributes["description"] = description
	}
	if awsAppARN := strings.TrimSpace(app.AWSApplicationARN); awsAppARN != "" {
		attributes["aws_application_arn"] = awsAppARN
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeResilienceHubApp,
		Name:               name,
		State:              strings.TrimSpace(app.Status),
		Tags:               cloneStringMap(app.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

// policyObservation builds the aws_resource observation for one resiliency
// policy. It records the policy tier, cost tier, data-location constraint, and
// the per-failure-type RPO/RTO targets only.
func policyObservation(boundary awscloud.Boundary, policy ResiliencyPolicy) awscloud.ResourceObservation {
	arn := strings.TrimSpace(policy.ARN)
	name := strings.TrimSpace(policy.Name)
	resourceID := policyResourceID(policy)
	attributes := map[string]any{
		"tier":                     strings.TrimSpace(policy.Tier),
		"estimated_cost_tier":      strings.TrimSpace(policy.EstimatedCostTier),
		"data_location_constraint": strings.TrimSpace(policy.DataLocationConstraint),
		"creation_time":            timeOrNil(policy.CreationTime),
	}
	if targets := failureTargetAttributes(policy.FailureTargets); len(targets) > 0 {
		attributes["failure_targets"] = targets
	}
	if description := strings.TrimSpace(policy.Description); description != "" {
		attributes["description"] = description
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeResilienceHubResiliencyPolicy,
		Name:               name,
		Tags:               cloneStringMap(policy.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

// componentObservation builds the aws_resource observation for one application
// component. Components have no API ARN, so the node is keyed by the
// application-qualified component id.
func componentObservation(
	boundary awscloud.Boundary,
	app App,
	component AppComponent,
) awscloud.ResourceObservation {
	appID := appResourceID(app)
	resourceID := componentResourceID(appID, component)
	name := strings.TrimSpace(component.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeResilienceHubAppComponent,
		Name:         name,
		Attributes: map[string]any{
			"component_type": strings.TrimSpace(component.Type),
			"app_arn":        strings.TrimSpace(app.ARN),
		},
		CorrelationAnchors: []string{resourceID, name},
		SourceRecordID:     resourceID,
	}
}

// inputSourceObservation builds the aws_resource observation for one application
// input source. It records the import type, source ARN, and reported resource
// count only.
func inputSourceObservation(
	boundary awscloud.Boundary,
	app App,
	source InputSource,
) awscloud.ResourceObservation {
	appID := appResourceID(app)
	resourceID := inputSourceResourceID(appID, source)
	name := strings.TrimSpace(source.SourceName)
	sourceARN := strings.TrimSpace(source.SourceARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          sourceARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeResilienceHubAppInputSource,
		Name:         name,
		Attributes: map[string]any{
			"import_type":    strings.TrimSpace(source.ImportType),
			"resource_count": source.ResourceCount,
			"app_arn":        strings.TrimSpace(app.ARN),
		},
		CorrelationAnchors: []string{sourceARN, resourceID, name},
		SourceRecordID:     resourceID,
	}
}

// assessmentObservation builds the aws_resource observation for one application
// assessment summary. It records the assessment outcome labels only, never the
// assessment result body or drift detail.
func assessmentObservation(boundary awscloud.Boundary, assessment Assessment) awscloud.ResourceObservation {
	arn := strings.TrimSpace(assessment.ARN)
	name := strings.TrimSpace(assessment.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeResilienceHubAppAssessment,
		Name:         name,
		State:        strings.TrimSpace(assessment.Status),
		Attributes: map[string]any{
			"app_arn":           strings.TrimSpace(assessment.AppARN),
			"compliance_status": strings.TrimSpace(assessment.ComplianceStatus),
			"drift_status":      strings.TrimSpace(assessment.DriftStatus),
			"invoker":           strings.TrimSpace(assessment.Invoker),
			"app_version":       strings.TrimSpace(assessment.AppVersion),
			"resiliency_score":  assessment.ResiliencyScore,
			"start_time":        timeOrNil(assessment.StartTime),
			"end_time":          timeOrNil(assessment.EndTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

// failureTargetAttributes renders the per-failure-type RPO/RTO targets as a
// nested attribute map, omitting it entirely when no targets are reported.
func failureTargetAttributes(targets map[string]FailureTarget) map[string]any {
	if len(targets) == 0 {
		return nil
	}
	output := make(map[string]any, len(targets))
	for key, target := range targets {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = map[string]any{
			"rpo_in_secs": target.RPOInSecs,
			"rto_in_secs": target.RTOInSecs,
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
