// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanrooms

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Clean Rooms metadata-only facts for one claimed account and
// region. It never reads or persists analysis-rule SQL, query bodies,
// allowed-column names, or member secrets, and never mutates Clean Rooms state.
// It reports collaborations, configured tables, and memberships plus the
// configured-table-to-Glue-table and membership-in-collaboration relationships.
type Scanner struct {
	// Client is the metadata-only Clean Rooms snapshot source.
	Client Client
}

// Scan observes Clean Rooms collaborations, configured tables, and memberships
// plus their direct dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cleanrooms scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCleanRooms:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCleanRooms
	default:
		return nil, fmt.Errorf("cleanrooms scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Clean Rooms metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, collaboration := range snapshot.Collaborations {
		envelope, err := awscloud.NewResourceEnvelope(collaborationObservation(boundary, collaboration))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, table := range snapshot.ConfiguredTables {
		next, err := configuredTableEnvelopes(boundary, table)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, membership := range snapshot.Memberships {
		next, err := membershipEnvelopes(boundary, membership)
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

func configuredTableEnvelopes(boundary awscloud.Boundary, table ConfiguredTable) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(configuredTableObservation(boundary, table))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := configuredTableGlueRelationship(boundary, table); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func membershipEnvelopes(boundary awscloud.Boundary, membership Membership) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(membershipObservation(boundary, membership))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := membershipCollaborationRelationship(boundary, membership); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func collaborationObservation(boundary awscloud.Boundary, collaboration Collaboration) awscloud.ResourceObservation {
	arn := strings.TrimSpace(collaboration.ARN)
	name := strings.TrimSpace(collaboration.Name)
	resourceID := collaborationResourceID(collaboration)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCleanRoomsCollaboration,
		Name:         name,
		State:        strings.TrimSpace(collaboration.MemberStatus),
		Tags:         cloneStringMap(collaboration.Tags),
		Attributes: map[string]any{
			"collaboration_id":     strings.TrimSpace(collaboration.ID),
			"creator_account_id":   strings.TrimSpace(collaboration.CreatorAccountID),
			"creator_display_name": strings.TrimSpace(collaboration.CreatorDisplayName),
			"member_status":        strings.TrimSpace(collaboration.MemberStatus),
			"analytics_engine":     strings.TrimSpace(collaboration.AnalyticsEngine),
			"create_time":          timeOrNil(collaboration.CreateTime),
			"update_time":          timeOrNil(collaboration.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func configuredTableObservation(boundary awscloud.Boundary, table ConfiguredTable) awscloud.ResourceObservation {
	arn := strings.TrimSpace(table.ARN)
	name := strings.TrimSpace(table.Name)
	resourceID := configuredTableResourceID(table)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCleanRoomsConfiguredTable,
		Name:         name,
		Tags:         cloneStringMap(table.Tags),
		Attributes: map[string]any{
			"configured_table_id":  strings.TrimSpace(table.ID),
			"analysis_method":      strings.TrimSpace(table.AnalysisMethod),
			"analysis_rule_types":  cloneStrings(table.AnalysisRuleTypes),
			"allowed_column_count": table.AllowedColumnCount,
			"table_reference_kind": strings.TrimSpace(table.TableReferenceKind),
			"glue_database_name":   strings.TrimSpace(table.GlueDatabaseName),
			"glue_table_name":      strings.TrimSpace(table.GlueTableName),
			"create_time":          timeOrNil(table.CreateTime),
			"update_time":          timeOrNil(table.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func membershipObservation(boundary awscloud.Boundary, membership Membership) awscloud.ResourceObservation {
	arn := strings.TrimSpace(membership.ARN)
	resourceID := membershipResourceID(membership)
	collaborationName := strings.TrimSpace(membership.CollaborationName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCleanRoomsMembership,
		Name:         collaborationName,
		State:        strings.TrimSpace(membership.Status),
		Tags:         cloneStringMap(membership.Tags),
		Attributes: map[string]any{
			"membership_id":                    strings.TrimSpace(membership.ID),
			"collaboration_arn":                strings.TrimSpace(membership.CollaborationARN),
			"collaboration_id":                 strings.TrimSpace(membership.CollaborationID),
			"collaboration_name":               collaborationName,
			"collaboration_creator_account_id": strings.TrimSpace(membership.CollaborationCreatorAccountID),
			"member_abilities":                 cloneStrings(membership.MemberAbilities),
			"status":                           strings.TrimSpace(membership.Status),
			"create_time":                      timeOrNil(membership.CreateTime),
			"update_time":                      timeOrNil(membership.UpdateTime),
		},
		CorrelationAnchors: []string{arn, collaborationName},
		SourceRecordID:     resourceID,
	}
}
