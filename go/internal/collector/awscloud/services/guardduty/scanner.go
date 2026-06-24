// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardduty

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS GuardDuty metadata facts for one claimed account and
// region. It never reads finding bodies, filter criteria, or IP/threat list
// contents, and it never mutates GuardDuty resources.
type Scanner struct {
	Client Client
}

// Scan observes GuardDuty detectors and metadata-only child resources through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("guardduty scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceGuardDuty:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceGuardDuty
	default:
		return nil, fmt.Errorf("guardduty scanner received service_kind %q", boundary.ServiceKind)
	}

	detectors, err := s.Client.ListDetectors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list GuardDuty detectors: %w", err)
	}
	var envelopes []facts.Envelope
	for _, detector := range detectors {
		detectorEnvelopes, err := detectorEnvelopes(boundary, detector)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, detectorEnvelopes...)
	}
	return envelopes, nil
}

func detectorEnvelopes(boundary awscloud.Boundary, detector Detector) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(detectorObservation(boundary, detector))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, member := range detector.Members {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			memberObservation(boundary, detector, member),
			memberRelationship(boundary, detector, member),
		)
		if err != nil {
			return nil, err
		}
	}
	for _, filter := range detector.Filters {
		envelope, err := awscloud.NewResourceEnvelope(filterObservation(boundary, detector, filter))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, destination := range detector.PublishingDestinations {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			publishingDestinationObservation(boundary, detector, destination),
			publishingDestinationRelationship(boundary, detector, destination),
		)
		if err != nil {
			return nil, err
		}
	}
	for _, set := range detector.ThreatIntelSets {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			threatIntelSetObservation(boundary, detector, set),
			threatIntelSetRelationship(boundary, detector, set),
		)
		if err != nil {
			return nil, err
		}
	}
	for _, set := range detector.IPSets {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			ipSetObservation(boundary, detector, set),
			ipSetRelationship(boundary, detector, set),
		)
		if err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendResourceAndRelationship(
	envelopes []facts.Envelope,
	resource awscloud.ResourceObservation,
	relationship awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	resourceEnvelope, err := awscloud.NewResourceEnvelope(resource)
	if err != nil {
		return nil, err
	}
	relationshipEnvelope, err := awscloud.NewRelationshipEnvelope(relationship)
	if err != nil {
		return nil, err
	}
	return append(envelopes, resourceEnvelope, relationshipEnvelope), nil
}

func detectorObservation(boundary awscloud.Boundary, detector Detector) awscloud.ResourceObservation {
	detectorID := strings.TrimSpace(detector.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   detectorID,
		ResourceType: awscloud.ResourceTypeGuardDutyDetector,
		Name:         detectorID,
		State:        strings.TrimSpace(detector.Status),
		Tags:         cloneStringMap(detector.Tags),
		Attributes: map[string]any{
			"created_at":                   strings.TrimSpace(detector.CreatedAt),
			"updated_at":                   strings.TrimSpace(detector.UpdatedAt),
			"finding_publishing_frequency": strings.TrimSpace(detector.FindingPublishingFrequency),
			"feature_configurations":       featureSummaries(detector.Features),
			"finding_counts_by_severity":   cloneInt64Map(detector.FindingCountsBySeverity),
			"finding_counts_by_type":       cloneInt64Map(detector.FindingCountsByType),
			"member_account_count":         len(detector.Members),
			"filter_count":                 len(detector.Filters),
			"publishing_destination_count": len(detector.PublishingDestinations),
			"threat_intel_set_count":       len(detector.ThreatIntelSets),
			"ip_set_count":                 len(detector.IPSets),
		},
		CorrelationAnchors: []string{detectorID},
		SourceRecordID:     detectorID,
	}
}

func memberObservation(boundary awscloud.Boundary, detector Detector, member MemberAccount) awscloud.ResourceObservation {
	resourceID := detectorChildID(detector.ID, "member", member.AccountID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeGuardDutyMemberAccount,
		Name:         strings.TrimSpace(member.AccountID),
		State:        strings.TrimSpace(member.RelationshipStatus),
		Attributes: map[string]any{
			"account_id":          strings.TrimSpace(member.AccountID),
			"administrator_id":    strings.TrimSpace(member.AdministratorID),
			"detector_id":         strings.TrimSpace(detector.ID),
			"member_detector_id":  strings.TrimSpace(member.DetectorID),
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
			"invited_at":          strings.TrimSpace(member.InvitedAt),
			"updated_at":          strings.TrimSpace(member.UpdatedAt),
		},
		CorrelationAnchors: []string{strings.TrimSpace(member.AccountID)},
		SourceRecordID:     resourceID,
	}
}

func filterObservation(boundary awscloud.Boundary, detector Detector, filter FilterSummary) awscloud.ResourceObservation {
	resourceID := detectorChildID(detector.ID, "filter", filter.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeGuardDutyFilter,
		Name:         strings.TrimSpace(filter.Name),
		Attributes: map[string]any{
			"detector_id": strings.TrimSpace(detector.ID),
		},
		SourceRecordID: resourceID,
	}
}

func publishingDestinationObservation(
	boundary awscloud.Boundary,
	detector Detector,
	destination PublishingDestination,
) awscloud.ResourceObservation {
	resourceID := detectorChildID(detector.ID, "publishing-destination", destination.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(destination.DestinationARN),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeGuardDutyPublishingDestination,
		Name:         strings.TrimSpace(destination.ID),
		State:        strings.TrimSpace(destination.Status),
		Tags:         cloneStringMap(destination.Tags),
		Attributes: map[string]any{
			"detector_id":       strings.TrimSpace(detector.ID),
			"destination_id":    strings.TrimSpace(destination.ID),
			"destination_type":  strings.TrimSpace(destination.DestinationType),
			"destination_arn":   strings.TrimSpace(destination.DestinationARN),
			"publishing_status": strings.TrimSpace(destination.Status),
		},
		SourceRecordID: resourceID,
	}
}

func threatIntelSetObservation(boundary awscloud.Boundary, detector Detector, set ThreatIntelSet) awscloud.ResourceObservation {
	resourceID := detectorChildID(detector.ID, "threat-intel-set", set.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeGuardDutyThreatIntelSet,
		Name:         strings.TrimSpace(set.Name),
		State:        strings.TrimSpace(set.Status),
		Tags:         cloneStringMap(set.Tags),
		Attributes: map[string]any{
			"detector_id":  strings.TrimSpace(detector.ID),
			"set_id":       strings.TrimSpace(set.ID),
			"format":       strings.TrimSpace(set.Format),
			"location_arn": strings.TrimSpace(set.LocationARN),
		},
		SourceRecordID: resourceID,
	}
}

func ipSetObservation(boundary awscloud.Boundary, detector Detector, set IPSet) awscloud.ResourceObservation {
	resourceID := detectorChildID(detector.ID, "ip-set", set.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeGuardDutyIPSet,
		Name:         strings.TrimSpace(set.Name),
		State:        strings.TrimSpace(set.Status),
		Tags:         cloneStringMap(set.Tags),
		Attributes: map[string]any{
			"detector_id":  strings.TrimSpace(detector.ID),
			"set_id":       strings.TrimSpace(set.ID),
			"format":       strings.TrimSpace(set.Format),
			"location_arn": strings.TrimSpace(set.LocationARN),
		},
		SourceRecordID: resourceID,
	}
}
