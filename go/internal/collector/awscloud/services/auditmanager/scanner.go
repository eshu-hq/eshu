// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Audit Manager metadata-only facts for one claimed account
// and region. It never reads collected audit evidence, evidence finder records,
// change logs, delegation comments, control narratives, or assessment report
// URLs, and never mutates Audit Manager state. It reports assessments,
// frameworks, and controls plus the assessment-to-framework, assessment-to-S3
// (assessment-reports destination), assessment-to-KMS-key (account settings
// key), and assessment-to-account (scope) relationships.
type Scanner struct {
	// Client is the metadata-only Audit Manager snapshot source.
	Client Client
}

// Scan observes Audit Manager assessments, frameworks, controls, and the direct
// framework, S3, KMS, and in-scope-account dependency metadata through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("auditmanager scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAuditManager:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAuditManager
	default:
		return nil, fmt.Errorf("auditmanager scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Audit Manager metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, assessment := range snapshot.Assessments {
		next, err := assessmentEnvelopes(boundary, assessment, snapshot.KMSKeyARN)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, framework := range snapshot.Frameworks {
		envelope, err := awscloud.NewResourceEnvelope(frameworkObservation(boundary, framework))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, control := range snapshot.Controls {
		envelope, err := awscloud.NewResourceEnvelope(controlObservation(boundary, control))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
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

func assessmentEnvelopes(
	boundary awscloud.Boundary,
	assessment Assessment,
	kmsKeyARN string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(assessmentObservation(boundary, assessment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	relationships := []*awscloud.RelationshipObservation{
		assessmentFrameworkRelationship(boundary, assessment),
		assessmentReportsS3Relationship(boundary, assessment),
		assessmentKMSRelationship(boundary, assessment, kmsKeyARN),
	}
	for _, accountID := range assessment.ScopeAccountIDs {
		relationships = append(relationships, assessmentAccountRelationship(boundary, assessment, accountID))
	}
	for _, relationship := range relationships {
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

func assessmentObservation(boundary awscloud.Boundary, assessment Assessment) awscloud.ResourceObservation {
	arn := strings.TrimSpace(assessment.ARN)
	name := strings.TrimSpace(assessment.Name)
	resourceID := assessmentResourceID(assessment)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAuditManagerAssessment,
		Name:         name,
		State:        strings.TrimSpace(assessment.Status),
		Tags:         cloneStringMap(assessment.Tags),
		Attributes: map[string]any{
			"assessment_id":            strings.TrimSpace(assessment.ID),
			"compliance_type":          strings.TrimSpace(assessment.ComplianceType),
			"status":                   strings.TrimSpace(assessment.Status),
			"framework_id":             strings.TrimSpace(assessment.FrameworkID),
			"reports_destination_type": strings.TrimSpace(assessment.ReportsDestinationType),
			"scope_account_ids":        cloneStrings(assessment.ScopeAccountIDs),
			"scope_service_names":      cloneStrings(assessment.ScopeServiceNames),
			"creation_time":            timeOrNil(assessment.CreationTime),
			"last_updated_time":        timeOrNil(assessment.LastUpdatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func frameworkObservation(boundary awscloud.Boundary, framework Framework) awscloud.ResourceObservation {
	arn := strings.TrimSpace(framework.ARN)
	name := strings.TrimSpace(framework.Name)
	resourceID := frameworkResourceID(framework)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAuditManagerFramework,
		Name:         name,
		Tags:         nil,
		Attributes: map[string]any{
			"framework_id":       strings.TrimSpace(framework.ID),
			"compliance_type":    strings.TrimSpace(framework.ComplianceType),
			"framework_type":     strings.TrimSpace(framework.Type),
			"control_sets_count": framework.ControlSetsCount,
			"controls_count":     framework.ControlsCount,
			"creation_time":      timeOrNil(framework.CreatedAt),
			"last_updated_time":  timeOrNil(framework.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func controlObservation(boundary awscloud.Boundary, control Control) awscloud.ResourceObservation {
	arn := strings.TrimSpace(control.ARN)
	name := strings.TrimSpace(control.Name)
	resourceID := controlResourceID(control)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAuditManagerControl,
		Name:         name,
		Attributes: map[string]any{
			"control_id":        strings.TrimSpace(control.ID),
			"control_type":      strings.TrimSpace(control.Type),
			"control_sources":   strings.TrimSpace(control.ControlSources),
			"creation_time":     timeOrNil(control.CreatedAt),
			"last_updated_time": timeOrNil(control.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
