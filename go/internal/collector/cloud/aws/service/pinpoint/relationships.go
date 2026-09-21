// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pinpoint

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// applicationHasSegmentRelationship records a Pinpoint segment's membership in
// its parent application. applicationID is the resource_id the application node
// publishes (its application id), so the edge joins the application node
// exactly. It returns nil when either endpoint identity is missing.
func applicationHasSegmentRelationship(
	boundary aws.Boundary,
	applicationID string,
	segment Segment,
) *aws.RelationshipObservation {
	segmentID := segmentResourceID(segment)
	applicationID = strings.TrimSpace(applicationID)
	if segmentID == "" || applicationID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipPinpointApplicationHasSegment,
		SourceResourceID: applicationID,
		TargetResourceID: segmentID,
		TargetARN:        strings.TrimSpace(segment.ARN),
		TargetType:       aws.ResourceTypePinpointSegment,
		SourceRecordID:   applicationID + "->" + aws.RelationshipPinpointApplicationHasSegment + ":" + segmentID,
	}
}

// channelInApplicationRelationship records a Pinpoint channel's membership in
// its parent application. applicationID is the resource_id the application node
// publishes, so the edge joins the application node exactly. It returns nil when
// either endpoint identity is missing.
func channelInApplicationRelationship(
	boundary aws.Boundary,
	applicationID string,
	channel Channel,
) *aws.RelationshipObservation {
	channelID := channelResourceID(channel)
	applicationID = strings.TrimSpace(applicationID)
	if channelID == "" || applicationID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipPinpointChannelInApplication,
		SourceResourceID: channelID,
		TargetResourceID: applicationID,
		TargetType:       aws.ResourceTypePinpointApplication,
		SourceRecordID:   channelID + "->" + aws.RelationshipPinpointChannelInApplication + ":" + applicationID,
	}
}

// emailChannelSESIdentityRelationship records a Pinpoint email channel's
// reported SES sending identity. Pinpoint reports the identity ARN; the scanner
// extracts the identity name (the verified email/domain) to match the
// resource_id the SES email-identity node publishes, which is the bare name and
// not an ARN. The edge therefore keys the bare name and does NOT set target_arn
// (an ARN-keyed target would not join the name-keyed SES node); the reported
// identity ARN is preserved as an attribute so the evidence is not lost. It
// returns nil when the channel reports no SES identity ARN or the ARN is not an
// SES identity ARN, so the edge is skipped rather than keyed to a dangling
// guess.
func emailChannelSESIdentityRelationship(
	boundary aws.Boundary,
	channel Channel,
) *aws.RelationshipObservation {
	identityName := sesIdentityNameFromARN(channel.SESIdentityARN)
	if identityName == "" {
		return nil
	}
	channelID := channelResourceID(channel)
	if channelID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipPinpointEmailChannelUsesSESIdentity,
		SourceResourceID: channelID,
		TargetResourceID: identityName,
		TargetType:       aws.ResourceTypeSESEmailIdentity,
		Attributes: map[string]any{
			"ses_identity_arn": strings.TrimSpace(channel.SESIdentityARN),
		},
		SourceRecordID: channelID + "->" + aws.RelationshipPinpointEmailChannelUsesSESIdentity + ":" + identityName,
	}
}

// emailChannelSESConfigurationSetRelationship records a Pinpoint email
// channel's reported SES configuration set. Pinpoint reports the configuration
// set name, which matches the resource_id the SES configuration-set node
// publishes, so the edge joins that node. It returns nil when no configuration
// set is configured.
func emailChannelSESConfigurationSetRelationship(
	boundary aws.Boundary,
	channel Channel,
) *aws.RelationshipObservation {
	configSet := strings.TrimSpace(channel.SESConfigurationSet)
	if configSet == "" {
		return nil
	}
	channelID := channelResourceID(channel)
	if channelID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipPinpointEmailChannelUsesSESConfigurationSet,
		SourceResourceID: channelID,
		TargetResourceID: configSet,
		TargetType:       aws.ResourceTypeSESConfigurationSet,
		SourceRecordID:   channelID + "->" + aws.RelationshipPinpointEmailChannelUsesSESConfigurationSet + ":" + configSet,
	}
}
