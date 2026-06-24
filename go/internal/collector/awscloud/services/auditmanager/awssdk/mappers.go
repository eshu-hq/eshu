// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsauditmanagertypes "github.com/aws/aws-sdk-go-v2/service/auditmanager/types"

	auditmanagerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/auditmanager"
)

// mapAssessment translates an Audit Manager GetAssessment response into the
// scanner-owned metadata model. It copies identity, compliance standard, status,
// the framework reference, the assessment-reports S3 destination location, and
// the in-scope account ids/service names only. It never copies the assessment
// description text, collected evidence, evidence folders, or delegation comments.
func mapAssessment(assessment *awsauditmanagertypes.Assessment) auditmanagerservice.Assessment {
	if assessment == nil {
		return auditmanagerservice.Assessment{}
	}
	mapped := auditmanagerservice.Assessment{
		ARN:  strings.TrimSpace(aws.ToString(assessment.Arn)),
		Tags: tagsFromSDK(assessment.Tags),
	}
	if metadata := assessment.Metadata; metadata != nil {
		mapped.ID = strings.TrimSpace(aws.ToString(metadata.Id))
		mapped.Name = strings.TrimSpace(aws.ToString(metadata.Name))
		mapped.ComplianceType = strings.TrimSpace(aws.ToString(metadata.ComplianceType))
		mapped.Status = strings.TrimSpace(string(metadata.Status))
		mapped.CreationTime = aws.ToTime(metadata.CreationTime)
		mapped.LastUpdatedTime = aws.ToTime(metadata.LastUpdated)
		applyReportsDestination(&mapped, metadata.AssessmentReportsDestination)
		applyScope(&mapped, metadata.Scope)
	}
	if framework := assessment.Framework; framework != nil {
		mapped.FrameworkARN = strings.TrimSpace(aws.ToString(framework.Arn))
		mapped.FrameworkID = strings.TrimSpace(aws.ToString(framework.Id))
	}
	return mapped
}

// applyReportsDestination copies the assessment-reports destination location
// reference (an s3:// URI and the destination type), never report content.
func applyReportsDestination(
	assessment *auditmanagerservice.Assessment,
	destination *awsauditmanagertypes.AssessmentReportsDestination,
) {
	if destination == nil {
		return
	}
	assessment.ReportsS3Destination = strings.TrimSpace(aws.ToString(destination.Destination))
	assessment.ReportsDestinationType = strings.TrimSpace(string(destination.DestinationType))
}

// applyScope copies the in-scope account ids and service names. AWS deprecated
// caller-specified services scope (the field is returned empty), so service
// names are usually absent; they are recorded as context only.
func applyScope(assessment *auditmanagerservice.Assessment, scope *awsauditmanagertypes.Scope) {
	if scope == nil {
		return
	}
	for _, account := range scope.AwsAccounts {
		if id := strings.TrimSpace(aws.ToString(account.Id)); id != "" {
			assessment.ScopeAccountIDs = append(assessment.ScopeAccountIDs, id)
		}
	}
	// AwsServices is deprecated by AWS: caller-specified services scope is ignored
	// and the field is returned empty. The scanner still reads it (defensively,
	// behind this nolint) so that if AWS ever populates it the names surface as
	// the documented scope_service_names context attribute, never as an edge.
	//nolint:staticcheck // SA1019: read deprecated scope services defensively; recorded as context only, never an edge.
	for _, service := range scope.AwsServices {
		if name := strings.TrimSpace(aws.ToString(service.ServiceName)); name != "" {
			assessment.ScopeServiceNames = append(assessment.ScopeServiceNames, name)
		}
	}
}

// mapFramework translates an Audit Manager framework metadata item into the
// scanner-owned model. It copies identity, compliance standard, framework type,
// and control/control-set counts only, never the framework description text.
func mapFramework(framework awsauditmanagertypes.AssessmentFrameworkMetadata) auditmanagerservice.Framework {
	return auditmanagerservice.Framework{
		ARN:              strings.TrimSpace(aws.ToString(framework.Arn)),
		ID:               strings.TrimSpace(aws.ToString(framework.Id)),
		Name:             strings.TrimSpace(aws.ToString(framework.Name)),
		ComplianceType:   strings.TrimSpace(aws.ToString(framework.ComplianceType)),
		Type:             strings.TrimSpace(string(framework.Type)),
		ControlSetsCount: framework.ControlSetsCount,
		ControlsCount:    framework.ControlsCount,
		CreatedAt:        aws.ToTime(framework.CreatedAt),
		LastUpdatedAt:    aws.ToTime(framework.LastUpdatedAt),
	}
}

// mapControl translates an Audit Manager control metadata item into the
// scanner-owned model. It copies identity, control type, and the evidence
// data-source category label only. It never copies control
// testing-information, action-plan instructions, or control-mapping source
// bodies, which are absent from the list metadata by construction.
func mapControl(control awsauditmanagertypes.ControlMetadata, controlType string) auditmanagerservice.Control {
	return auditmanagerservice.Control{
		ARN:            strings.TrimSpace(aws.ToString(control.Arn)),
		ID:             strings.TrimSpace(aws.ToString(control.Id)),
		Name:           strings.TrimSpace(aws.ToString(control.Name)),
		Type:           strings.TrimSpace(controlType),
		ControlSources: strings.TrimSpace(aws.ToString(control.ControlSources)),
		CreatedAt:      aws.ToTime(control.CreatedAt),
		LastUpdatedAt:  aws.ToTime(control.LastUpdatedAt),
	}
}

// tagsFromSDK returns a trimmed-key copy of the SDK tag map, or nil when empty.
func tagsFromSDK(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		output[key] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
