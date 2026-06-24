// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityhub

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS Security Hub metadata facts for one claimed account and
// region.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes Security Hub configuration, standards, controls, member
// accounts, action targets, insight summaries, and aggregate finding posture.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("securityhub scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("securityhub scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSecurityHub:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceSecurityHub
	default:
		return nil, fmt.Errorf("securityhub scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Security Hub metadata: %w", err)
	}
	controlResourceIDs := controlIDsByResourceID(snapshot.Standards)
	var envelopes []facts.Envelope
	for _, observation := range s.resourceObservations(boundary, snapshot) {
		envelope, err := awscloud.NewResourceEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, observation := range relationshipObservations(boundary, snapshot, controlResourceIDs) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (s Scanner) resourceObservations(
	boundary awscloud.Boundary,
	snapshot Snapshot,
) []awscloud.ResourceObservation {
	observations := []awscloud.ResourceObservation{hubObservation(boundary, snapshot.Hub)}
	for _, member := range snapshot.Members {
		if strings.TrimSpace(member.AccountID) == "" {
			continue
		}
		observations = append(observations, memberObservation(boundary, member))
	}
	for _, standard := range snapshot.Standards {
		observations = append(observations, standardObservation(boundary, standard))
		for _, control := range standard.Controls {
			observations = append(observations, controlObservation(boundary, standard, control))
		}
	}
	for _, target := range snapshot.ActionTargets {
		observations = append(observations, s.actionTargetObservation(boundary, target))
	}
	for _, insight := range snapshot.Insights {
		observations = append(observations, insightObservation(boundary, insight))
	}
	for _, count := range snapshot.FindingCounts {
		observations = append(observations, findingCountObservation(boundary, count))
	}
	return observations
}

func hubObservation(boundary awscloud.Boundary, hub Hub) awscloud.ResourceObservation {
	hubARN := strings.TrimSpace(hub.ARN)
	resourceID := firstNonEmpty(hubARN, "securityhub:"+boundary.AccountID+":"+boundary.Region+":hub")
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          hubARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubHub,
		Name:         "default",
		State:        "enabled",
		Tags:         cloneStringMap(hub.Tags),
		Attributes: map[string]any{
			"administrator_account_id":  strings.TrimSpace(hub.AdministratorAccountID),
			"administrator_status":      strings.TrimSpace(hub.AdministratorStatus),
			"auto_enable_controls":      hub.AutoEnableControls,
			"control_finding_generator": strings.TrimSpace(hub.ControlFindingGenerator),
			"member_enumeration_status": strings.TrimSpace(hub.MemberEnumerationStatus),
			"subscribed_at":             timeOrNil(hub.SubscribedAt),
		},
		CorrelationAnchors: []string{hubARN, resourceID},
		SourceRecordID:     resourceID,
	}
}

func memberObservation(boundary awscloud.Boundary, member Member) awscloud.ResourceObservation {
	accountID := strings.TrimSpace(member.AccountID)
	resourceID := "securityhub_member:" + accountID
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubMemberAccount,
		Name:         accountID,
		State:        strings.TrimSpace(member.Status),
		Attributes: map[string]any{
			"administrator_id": strings.TrimSpace(member.AdministratorID),
			"invited_at":       timeOrNil(member.InvitedAt),
			"updated_at":       timeOrNil(member.UpdatedAt),
		},
		CorrelationAnchors: []string{accountID, resourceID},
		SourceRecordID:     resourceID,
	}
}

func standardObservation(boundary awscloud.Boundary, standard Standard) awscloud.ResourceObservation {
	resourceID := standardResourceID(standard)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(standard.SubscriptionARN),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubStandard,
		Name:         lastPathElement(firstNonEmpty(standard.ARN, standard.SubscriptionARN)),
		State:        strings.TrimSpace(standard.Status),
		Tags:         cloneStringMap(standard.Tags),
		Attributes: map[string]any{
			"control_finding_generator": strings.TrimSpace(standard.ControlFindingGenerator),
			"controls_updatable":        strings.TrimSpace(standard.ControlsUpdatable),
			"standards_arn":             strings.TrimSpace(standard.ARN),
			"standards_input_keys":      cloneStrings(standard.StandardsInputKeys),
			"status_reason_code":        strings.TrimSpace(standard.StatusReasonCode),
		},
		CorrelationAnchors: []string{standard.ARN, standard.SubscriptionARN, resourceID},
		SourceRecordID:     resourceID,
	}
}

func controlObservation(boundary awscloud.Boundary, standard Standard, control Control) awscloud.ResourceObservation {
	controlARN := strings.TrimSpace(control.ARN)
	resourceID := controlResourceID(standard, control)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          controlARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubControl,
		Name:         strings.TrimSpace(control.ID),
		State:        strings.TrimSpace(control.ControlStatus),
		Attributes: map[string]any{
			"compliance_counts":         cloneInt64Map(control.ComplianceCounts),
			"control_id":                strings.TrimSpace(control.ID),
			"related_requirements":      cloneStrings(control.Related),
			"severity_rating":           strings.TrimSpace(control.SeverityRating),
			"standard_subscription_arn": strings.TrimSpace(standard.SubscriptionARN),
			"standards_arn":             strings.TrimSpace(standard.ARN),
			"title":                     strings.TrimSpace(control.Title),
		},
		CorrelationAnchors: []string{controlARN, control.ID, resourceID},
		SourceRecordID:     resourceID,
	}
}

func (s Scanner) actionTargetObservation(
	boundary awscloud.Boundary,
	target ActionTarget,
) awscloud.ResourceObservation {
	actionARN := strings.TrimSpace(target.ARN)
	name := strings.TrimSpace(target.Name)
	resourceID := firstNonEmpty(actionARN, "securityhub_action:"+name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          actionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubActionTarget,
		Name:         name,
		Attributes: map[string]any{
			"description": s.redactedDescription(target),
		},
		CorrelationAnchors: []string{actionARN, name},
		SourceRecordID:     resourceID,
	}
}

func insightObservation(boundary awscloud.Boundary, insight Insight) awscloud.ResourceObservation {
	insightARN := strings.TrimSpace(insight.ARN)
	name := strings.TrimSpace(insight.Name)
	resourceID := firstNonEmpty(insightARN, "securityhub_insight:"+name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          insightARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubInsight,
		Name:         name,
		Attributes: map[string]any{
			"group_by_attribute": strings.TrimSpace(insight.GroupByAttribute),
		},
		CorrelationAnchors: []string{insightARN, name},
		SourceRecordID:     resourceID,
	}
}

func findingCountObservation(boundary awscloud.Boundary, count FindingCount) awscloud.ResourceObservation {
	resourceID := strings.Join([]string{
		"securityhub_finding_aggregate",
		strings.TrimSpace(count.StandardID),
		strings.TrimSpace(count.ControlID),
		strings.TrimSpace(count.ComplianceStatus),
		strings.TrimSpace(count.SeverityLabel),
		strings.TrimSpace(count.WorkflowStatus),
	}, ":")
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityHubFindingAggregate,
		Name:         strings.TrimSpace(count.ControlID),
		Attributes: map[string]any{
			"compliance_status": strings.TrimSpace(count.ComplianceStatus),
			"control_id":        strings.TrimSpace(count.ControlID),
			"count":             count.Count,
			"severity_label":    strings.TrimSpace(count.SeverityLabel),
			"standard_id":       strings.TrimSpace(count.StandardID),
			"workflow_status":   strings.TrimSpace(count.WorkflowStatus),
		},
		CorrelationAnchors: []string{count.StandardID, count.ControlID},
		SourceRecordID:     resourceID,
	}
}

func relationshipObservations(
	boundary awscloud.Boundary,
	snapshot Snapshot,
	controlResourceIDs map[string]string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	hubID := hubResourceID(boundary, snapshot.Hub)
	for _, member := range snapshot.Members {
		memberID := "securityhub_member:" + strings.TrimSpace(member.AccountID)
		if memberID == "securityhub_member:" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipSecurityHubHubHasMember,
			SourceResourceID: hubID,
			SourceARN:        strings.TrimSpace(snapshot.Hub.ARN),
			TargetResourceID: memberID,
			TargetType:       awscloud.ResourceTypeSecurityHubMemberAccount,
			Attributes: map[string]any{
				"administrator_id": strings.TrimSpace(member.AdministratorID),
				"member_status":    strings.TrimSpace(member.Status),
			},
			SourceRecordID: hubID + "->" + memberID,
		})
	}
	for _, standard := range snapshot.Standards {
		standardID := standardResourceID(standard)
		for _, control := range standard.Controls {
			controlID := controlResourceID(standard, control)
			if standardID == "" || controlID == "" {
				continue
			}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipSecurityHubStandardHasControl,
				SourceResourceID: standardID,
				SourceARN:        strings.TrimSpace(standard.SubscriptionARN),
				TargetResourceID: controlID,
				TargetARN:        strings.TrimSpace(control.ARN),
				TargetType:       awscloud.ResourceTypeSecurityHubControl,
				Attributes: map[string]any{
					"control_id": strings.TrimSpace(control.ID),
				},
				SourceRecordID: standardID + "->" + controlID,
			})
		}
	}
	for _, insight := range snapshot.Insights {
		insightName := strings.TrimSpace(insight.Name)
		insightID := firstNonEmpty(insight.ARN, "securityhub_insight:"+insightName)
		if !groupsByControl(insight.GroupByAttribute) {
			continue
		}
		for _, controlID := range insight.ControlIDs {
			targetID := controlResourceIDs[strings.TrimSpace(controlID)]
			if targetID == "" {
				continue
			}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipSecurityHubInsightGroupsControl,
				SourceResourceID: insightID,
				SourceARN:        strings.TrimSpace(insight.ARN),
				TargetResourceID: targetID,
				TargetType:       awscloud.ResourceTypeSecurityHubControl,
				Attributes: map[string]any{
					"group_by_attribute": strings.TrimSpace(insight.GroupByAttribute),
				},
				SourceRecordID: insightID + "->" + targetID,
			})
		}
	}
	return observations
}

func (s Scanner) redactedDescription(target ActionTarget) map[string]any {
	description := strings.TrimSpace(target.Description)
	if description == "" {
		return nil
	}
	source := "securityhub.action_target.description." + strings.TrimSpace(target.Name)
	return awscloud.RedactString(description, source, s.RedactionKey)
}

func controlIDsByResourceID(standards []Standard) map[string]string {
	output := make(map[string]string)
	for _, standard := range standards {
		for _, control := range standard.Controls {
			controlID := strings.TrimSpace(control.ID)
			if controlID == "" {
				continue
			}
			output[controlID] = controlResourceID(standard, control)
		}
	}
	return output
}

func hubResourceID(boundary awscloud.Boundary, hub Hub) string {
	return firstNonEmpty(hub.ARN, "securityhub:"+boundary.AccountID+":"+boundary.Region+":hub")
}

func standardResourceID(standard Standard) string {
	return firstNonEmpty(standard.SubscriptionARN, standard.ARN)
}

func controlResourceID(standard Standard, control Control) string {
	return firstNonEmpty(control.ARN, standardResourceID(standard)+"/"+strings.TrimSpace(control.ID), control.ID)
}

func groupsByControl(attribute string) bool {
	normalized := strings.ToLower(strings.NewReplacer(".", "", "_", "").Replace(strings.TrimSpace(attribute)))
	return normalized == "compliancesecuritycontrolid" || normalized == "securitycontrolid"
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func lastPathElement(value string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
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
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			output = append(output, trimmed)
		}
	}
	sort.Strings(output)
	return output
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]int64, len(input))
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
