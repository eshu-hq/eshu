// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardduty

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func memberRelationship(
	boundary awscloud.Boundary,
	detector Detector,
	member MemberAccount,
) awscloud.RelationshipObservation {
	detectorID := strings.TrimSpace(detector.ID)
	memberID := detectorChildID(detectorID, "member", member.AccountID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGuardDutyDetectorHasMemberAccount,
		SourceResourceID: detectorID,
		TargetResourceID: memberID,
		TargetType:       awscloud.ResourceTypeGuardDutyMemberAccount,
		Attributes: map[string]any{
			"account_id":          strings.TrimSpace(member.AccountID),
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
		},
		SourceRecordID: detectorID + "->" + memberID,
	}
}

func publishingDestinationRelationship(
	boundary awscloud.Boundary,
	detector Detector,
	destination PublishingDestination,
) awscloud.RelationshipObservation {
	detectorID := strings.TrimSpace(detector.ID)
	destinationID := detectorChildID(detectorID, "publishing-destination", destination.ID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGuardDutyDetectorPublishesToDestination,
		SourceResourceID: detectorID,
		TargetResourceID: destinationID,
		TargetARN:        strings.TrimSpace(destination.DestinationARN),
		TargetType:       awscloud.ResourceTypeGuardDutyPublishingDestination,
		Attributes: map[string]any{
			"destination_type": strings.TrimSpace(destination.DestinationType),
			"status":           strings.TrimSpace(destination.Status),
		},
		SourceRecordID: detectorID + "->" + destinationID,
	}
}

func threatIntelSetRelationship(
	boundary awscloud.Boundary,
	detector Detector,
	set ThreatIntelSet,
) awscloud.RelationshipObservation {
	detectorID := strings.TrimSpace(detector.ID)
	setID := detectorChildID(detectorID, "threat-intel-set", set.ID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGuardDutyDetectorUsesThreatIntelSet,
		SourceResourceID: detectorID,
		TargetResourceID: setID,
		TargetType:       awscloud.ResourceTypeGuardDutyThreatIntelSet,
		Attributes: map[string]any{
			"format": strings.TrimSpace(set.Format),
			"status": strings.TrimSpace(set.Status),
		},
		SourceRecordID: detectorID + "->" + setID,
	}
}

func ipSetRelationship(
	boundary awscloud.Boundary,
	detector Detector,
	set IPSet,
) awscloud.RelationshipObservation {
	detectorID := strings.TrimSpace(detector.ID)
	setID := detectorChildID(detectorID, "ip-set", set.ID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGuardDutyDetectorUsesIPSet,
		SourceResourceID: detectorID,
		TargetResourceID: setID,
		TargetType:       awscloud.ResourceTypeGuardDutyIPSet,
		Attributes: map[string]any{
			"format": strings.TrimSpace(set.Format),
			"status": strings.TrimSpace(set.Status),
		},
		SourceRecordID: detectorID + "->" + setID,
	}
}

func featureSummaries(features []FeatureConfiguration) []map[string]any {
	if len(features) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(features))
	for _, feature := range features {
		output = append(output, map[string]any{
			"name":                     strings.TrimSpace(feature.Name),
			"status":                   strings.TrimSpace(feature.Status),
			"updated_at":               feature.UpdatedAt,
			"additional_configuration": featureSummaries(feature.AdditionalConfiguration),
		})
	}
	return output
}

func detectorChildID(detectorID string, kind string, id string) string {
	return strings.Join([]string{
		strings.TrimSpace(detectorID),
		strings.TrimSpace(kind),
		strings.TrimSpace(id),
	}, "/")
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
