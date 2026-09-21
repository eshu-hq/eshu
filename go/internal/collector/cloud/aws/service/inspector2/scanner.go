// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inspector2

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Inspector v2 metadata facts for one claimed account and
// region. It never reads finding bodies, never persists filter criteria
// expressions, and never mutates Inspector resources.
type Scanner struct {
	Client Client
}

// Scan observes Inspector v2 account status, enabled scan features, member
// accounts, findings filter names, and CIS scan configuration metadata through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("inspector2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceInspector2:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceInspector2
	default:
		return nil, fmt.Errorf("inspector2 scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	account, err := s.Client.AccountStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("get Inspector v2 account status: %w", err)
	}
	accountEnvelopes, err := accountEnvelopes(boundary, account)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, accountEnvelopes...)

	members, err := s.Client.ListMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Inspector v2 members: %w", err)
	}
	for _, member := range members {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			memberObservation(boundary, member),
			memberRelationship(boundary, member),
		)
		if err != nil {
			return nil, err
		}
	}

	filters, err := s.Client.ListFilters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Inspector v2 filters: %w", err)
	}
	for _, filter := range filters {
		envelope, err := aws.NewResourceEnvelope(filterObservation(boundary, filter))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	cisConfigs, err := s.Client.ListCisScanConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Inspector v2 CIS scan configurations: %w", err)
	}
	for _, config := range cisConfigs {
		resource, err := aws.NewResourceEnvelope(cisConfigObservation(boundary, config))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, targetAccount := range config.TargetAccounts {
			relationship, ok := cisTargetRelationship(boundary, config, targetAccount)
			if !ok {
				continue
			}
			envelope, err := aws.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	return envelopes, nil
}

// accountEnvelopes emits the account-status resource. Feature enablement stays
// on the account resource's "features" attribute rather than fanning out into
// relationships to synthetic feature-status targets that no resource backs.
func accountEnvelopes(boundary aws.Boundary, account AccountStatus) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(accountObservation(boundary, account))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

func appendResourceAndRelationship(
	envelopes []facts.Envelope,
	resource aws.ResourceObservation,
	relationship aws.RelationshipObservation,
) ([]facts.Envelope, error) {
	resourceEnvelope, err := aws.NewResourceEnvelope(resource)
	if err != nil {
		return nil, err
	}
	relationshipEnvelope, err := aws.NewRelationshipEnvelope(relationship)
	if err != nil {
		return nil, err
	}
	return append(envelopes, resourceEnvelope, relationshipEnvelope), nil
}

func accountObservation(boundary aws.Boundary, account AccountStatus) aws.ResourceObservation {
	accountID := firstNonEmpty(account.AccountID, boundary.AccountID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   accountResourceID(accountID),
		ResourceType: aws.ResourceTypeInspector2Account,
		Name:         accountID,
		State:        strings.TrimSpace(account.Status),
		Attributes: map[string]any{
			"account_id": accountID,
			"status":     strings.TrimSpace(account.Status),
			"features":   featureSummaries(account.Features),
		},
		CorrelationAnchors: []string{accountID},
		SourceRecordID:     accountResourceID(accountID),
	}
}

func memberObservation(boundary aws.Boundary, member MemberAccount) aws.ResourceObservation {
	memberID := strings.TrimSpace(member.AccountID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   memberResourceID(memberID),
		ResourceType: aws.ResourceTypeInspector2MemberAccount,
		Name:         memberID,
		State:        strings.TrimSpace(member.RelationshipStatus),
		Attributes: map[string]any{
			"account_id":          memberID,
			"administrator_id":    strings.TrimSpace(member.AdministratorID),
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
			"updated_at":          strings.TrimSpace(member.UpdatedAt),
		},
		CorrelationAnchors: []string{memberID},
		SourceRecordID:     memberResourceID(memberID),
	}
}

func filterObservation(boundary aws.Boundary, filter FilterSummary) aws.ResourceObservation {
	filterARN := strings.TrimSpace(filter.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          filterARN,
		ResourceID:   firstNonEmpty(filterARN, filter.Name),
		ResourceType: aws.ResourceTypeInspector2Filter,
		Name:         strings.TrimSpace(filter.Name),
		Attributes: map[string]any{
			"action":   strings.TrimSpace(filter.Action),
			"owner_id": strings.TrimSpace(filter.OwnerID),
		},
		CorrelationAnchors: []string{filterARN},
		SourceRecordID:     firstNonEmpty(filterARN, filter.Name),
	}
}

func cisConfigObservation(boundary aws.Boundary, config CisScanConfiguration) aws.ResourceObservation {
	configARN := strings.TrimSpace(config.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          configARN,
		ResourceID:   firstNonEmpty(configARN, config.Name),
		ResourceType: aws.ResourceTypeInspector2CisScanConfiguration,
		Name:         strings.TrimSpace(config.Name),
		Tags:         cloneStringMap(config.Tags),
		Attributes: map[string]any{
			"owner_id":             strings.TrimSpace(config.OwnerID),
			"security_level":       strings.TrimSpace(config.SecurityLevel),
			"schedule_kind":        strings.TrimSpace(config.ScheduleKind),
			"target_account_count": len(config.TargetAccounts),
		},
		CorrelationAnchors: []string{configARN},
		SourceRecordID:     firstNonEmpty(configARN, config.Name),
	}
}

func featureSummaries(features []FeatureStatus) []map[string]any {
	if len(features) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(features))
	for _, feature := range features {
		output = append(output, map[string]any{
			"feature": strings.TrimSpace(feature.Feature),
			"status":  strings.TrimSpace(feature.Status),
		})
	}
	return output
}

func accountResourceID(accountID string) string {
	return "inspector2/account/" + strings.TrimSpace(accountID)
}

func memberResourceID(accountID string) string {
	return "inspector2/member/" + strings.TrimSpace(accountID)
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
