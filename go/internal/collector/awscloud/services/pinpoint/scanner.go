// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pinpoint

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Pinpoint metadata-only facts for one claimed account and
// region. It reports applications, segments, and channel settings plus the
// application-to-segment, channel-in-application, and (for the email channel)
// channel-to-SES-identity and channel-to-SES-configuration-set relationships. It
// never reads endpoint records, addresses, message or template content, segment
// targeting criteria values, or channel credentials, and never sends a message
// or mutates Pinpoint state.
type Scanner struct {
	// Client is the metadata-only Pinpoint snapshot source.
	Client Client
}

// Scan observes Pinpoint applications, their segments and channels, and the
// email-channel SES dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("pinpoint scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServicePinpoint:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServicePinpoint
	default:
		return nil, fmt.Errorf("pinpoint scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Pinpoint applications: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, application := range snapshot.Applications {
		next, err := applicationEnvelopes(boundary, application)
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

func applicationEnvelopes(boundary awscloud.Boundary, application Application) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(applicationObservation(boundary, application))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	applicationID := applicationResourceID(application)
	for _, segment := range application.Segments {
		next, err := segmentEnvelopes(boundary, applicationID, segment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, channel := range application.Channels {
		next, err := channelEnvelopes(boundary, applicationID, channel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func segmentEnvelopes(boundary awscloud.Boundary, applicationID string, segment Segment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(segmentObservation(boundary, segment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := applicationHasSegmentRelationship(boundary, applicationID, segment); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func channelEnvelopes(boundary awscloud.Boundary, applicationID string, channel Channel) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(channelObservation(boundary, channel))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		channelInApplicationRelationship(boundary, applicationID, channel),
		emailChannelSESIdentityRelationship(boundary, channel),
		emailChannelSESConfigurationSetRelationship(boundary, channel),
	} {
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

func applicationObservation(boundary awscloud.Boundary, application Application) awscloud.ResourceObservation {
	arn := strings.TrimSpace(application.ARN)
	name := strings.TrimSpace(application.Name)
	resourceID := applicationResourceID(application)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypePinpointApplication,
		Name:         name,
		Tags:         cloneStringMap(application.Tags),
		Attributes: map[string]any{
			"application_id": strings.TrimSpace(application.ID),
			"creation_time":  timeOrNil(application.CreationTime),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(application.ID), name},
		SourceRecordID:     resourceID,
	}
}

func segmentObservation(boundary awscloud.Boundary, segment Segment) awscloud.ResourceObservation {
	arn := strings.TrimSpace(segment.ARN)
	name := strings.TrimSpace(segment.Name)
	resourceID := segmentResourceID(segment)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypePinpointSegment,
		Name:         name,
		Tags:         cloneStringMap(segment.Tags),
		Attributes: map[string]any{
			"segment_id":         strings.TrimSpace(segment.ID),
			"application_id":     strings.TrimSpace(segment.ApplicationID),
			"segment_type":       strings.TrimSpace(segment.SegmentType),
			"version":            segment.Version,
			"imported_from_s3":   segment.ImportedFromS3,
			"import_format":      strings.TrimSpace(segment.ImportFormat),
			"import_size":        segment.ImportSize,
			"creation_time":      timeOrNil(segment.CreationTime),
			"last_modified_time": timeOrNil(segment.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(segment.ID), name},
		SourceRecordID:     resourceID,
	}
}

func channelObservation(boundary awscloud.Boundary, channel Channel) awscloud.ResourceObservation {
	resourceID := channelResourceID(channel)
	kind := strings.TrimSpace(channel.ChannelType)
	attributes := map[string]any{
		"application_id":     strings.TrimSpace(channel.ApplicationID),
		"channel_type":       kind,
		"enabled":            channel.Enabled,
		"archived":           channel.Archived,
		"version":            channel.Version,
		"creation_time":      timeOrNil(channel.CreationTime),
		"last_modified_time": timeOrNil(channel.LastModifiedTime),
	}
	if configSet := strings.TrimSpace(channel.SESConfigurationSet); configSet != "" {
		attributes["ses_configuration_set"] = configSet
	}
	if identityName := sesIdentityNameFromARN(channel.SESIdentityARN); identityName != "" {
		attributes["ses_identity"] = identityName
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypePinpointChannel,
		Name:               kind,
		State:              channelState(channel),
		Attributes:         attributes,
		CorrelationAnchors: []string{resourceID},
		SourceRecordID:     resourceID,
	}
}

// channelState reports a coarse channel state derived from the enabled flag so
// the channel node carries a queryable lifecycle hint without any credential.
func channelState(channel Channel) string {
	if channel.Enabled {
		return "ENABLED"
	}
	return "DISABLED"
}
