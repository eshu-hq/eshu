// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon AppFlow metadata-only facts for one claimed account and
// region. It never starts, stops, or runs flows, never reads flow run records
// or field mappings (task transforms, which can encode literal data values),
// and never reads connector credentials or OAuth tokens. The only credential
// reference it records is the Secrets Manager credentials ARN, used to join the
// connector profile to its secret node.
type Scanner struct {
	Client Client
}

// Scan observes AppFlow flows and connector profiles through the configured
// client and emits resource facts plus relationship evidence for flow-to-S3,
// flow-to-connector-profile, flow-to-KMS-key, and connector-profile-to-secret
// edges. Field mappings, flow run records, connector credentials, and OAuth
// tokens stay outside the scanner contract.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("appflow scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAppFlow:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAppFlow
	default:
		return nil, fmt.Errorf("appflow scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	flows, err := s.Client.ListFlows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AppFlow flows: %w", err)
	}
	for _, flow := range flows {
		next, err := flowEnvelopes(boundary, flow)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	profiles, err := s.Client.ListConnectorProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AppFlow connector profiles: %w", err)
	}
	for _, profile := range profiles {
		next, err := connectorProfileEnvelopes(boundary, profile)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

func flowEnvelopes(boundary awscloud.Boundary, flow Flow) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(flowObservation(boundary, flow))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	scalarRelationships := []*awscloud.RelationshipObservation{
		flowS3SourceRelationship(boundary, flow),
		flowKMSKeyRelationship(boundary, flow),
	}
	for _, relationship := range scalarRelationships {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	listRelationships := flowS3DestinationRelationships(boundary, flow)
	listRelationships = append(listRelationships, flowConnectorProfileRelationships(boundary, flow)...)
	for _, relationship := range listRelationships {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func connectorProfileEnvelopes(boundary awscloud.Boundary, profile ConnectorProfile) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(connectorProfileObservation(boundary, profile))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := connectorProfileSecretRelationship(boundary, profile); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func flowObservation(boundary awscloud.Boundary, flow Flow) awscloud.ResourceObservation {
	flowID := flowResourceID(flow)
	name := strings.TrimSpace(flow.Name)
	arn := strings.TrimSpace(flow.ARN)
	anchors := []string{flowID}
	if name != "" && name != flowID {
		anchors = append(anchors, name)
	}
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   flowID,
		ResourceType: awscloud.ResourceTypeAppFlowFlow,
		Name:         name,
		State:        strings.TrimSpace(flow.Status),
		Attributes: map[string]any{
			"description":                        strings.TrimSpace(flow.Description),
			"source_connector_type":              strings.TrimSpace(flow.SourceConnectorType),
			"destination_connector_type":         strings.TrimSpace(flow.DestinationConnectorType),
			"source_connector_profile_name":      strings.TrimSpace(flow.SourceConnectorProfileName),
			"destination_connector_profile_name": strings.TrimSpace(flow.DestinationConnectorProfileName),
			"trigger_type":                       strings.TrimSpace(flow.TriggerType),
			"created_at":                         timeOrNil(flow.CreatedAt),
			"last_updated_at":                    timeOrNil(flow.LastUpdatedAt),
		},
		CorrelationAnchors: anchors,
		SourceRecordID:     flowID,
	}
}

func connectorProfileObservation(boundary awscloud.Boundary, profile ConnectorProfile) awscloud.ResourceObservation {
	name := strings.TrimSpace(profile.Name)
	arn := strings.TrimSpace(profile.ARN)
	anchors := []string{name}
	if arn != "" {
		anchors = append(anchors, arn)
	}
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeAppFlowConnectorProfile,
		Name:         name,
		State:        strings.TrimSpace(profile.ConnectionMode),
		Attributes: map[string]any{
			"connector_type":  strings.TrimSpace(profile.ConnectorType),
			"connector_label": strings.TrimSpace(profile.ConnectorLabel),
			"connection_mode": strings.TrimSpace(profile.ConnectionMode),
			"created_at":      timeOrNil(profile.CreatedAt),
			"last_updated_at": timeOrNil(profile.LastUpdatedAt),
		},
		CorrelationAnchors: anchors,
		SourceRecordID:     name,
	}
}
