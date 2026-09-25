// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ses

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// identityConfigurationSetRelationship records an SES email identity's reported
// default configuration set. SES reports the configuration set NAME, which is
// the resource_id the SES configuration-set node publishes, so the edge joins
// that node exactly. It returns nil when the identity reports no default set.
func identityConfigurationSetRelationship(
	boundary aws.Boundary,
	identity EmailIdentity,
) *aws.RelationshipObservation {
	setName := strings.TrimSpace(identity.ConfigurationSetName)
	if setName == "" {
		return nil
	}
	sourceID := identityResourceID(identity)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSESEmailIdentityUsesConfigurationSet,
		SourceResourceID: sourceID,
		SourceARN:        identityARN(boundary, identity),
		TargetResourceID: setName,
		TargetType:       aws.ResourceTypeSESConfigurationSet,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipSESEmailIdentityUsesConfigurationSet + ":" + setName,
	}
}

// identityDKIMKMSRelationship records an SES email identity's reported DKIM
// customer KMS key dependency. SES v2 does not surface a customer KMS key on the
// DKIM attributes today, so this edge is emitted only on the defensive path
// where AWS ever reports a key identifier. The target is keyed by the reported
// key identifier the KMS scanner publishes (a key ARN). It returns nil when no
// key identifier is reported.
func identityDKIMKMSRelationship(
	boundary aws.Boundary,
	identity EmailIdentity,
) *aws.RelationshipObservation {
	keyID := strings.TrimSpace(identity.DKIMKMSKeyID)
	if keyID == "" {
		return nil
	}
	sourceID := identityResourceID(identity)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(keyID) {
		targetARN = keyID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSESEmailIdentityDKIMUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        identityARN(boundary, identity),
		TargetResourceID: keyID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeKMSKey,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipSESEmailIdentityDKIMUsesKMSKey + ":" + keyID,
	}
}

// configurationSetDedicatedIPPoolRelationship records a configuration set's
// reported sending (dedicated IP) pool. SES reports the pool NAME, which is the
// resource_id the SES dedicated-IP-pool node publishes, so the edge joins that
// node. It returns nil when the set sends through the shared pool.
func configurationSetDedicatedIPPoolRelationship(
	boundary aws.Boundary,
	set ConfigurationSet,
) *aws.RelationshipObservation {
	poolName := strings.TrimSpace(set.SendingPoolName)
	if poolName == "" {
		return nil
	}
	sourceID := configurationSetResourceID(set)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSESConfigurationSetUsesDedicatedIPPool,
		SourceResourceID: sourceID,
		SourceARN:        configurationSetARN(boundary, set),
		TargetResourceID: poolName,
		TargetType:       aws.ResourceTypeSESDedicatedIPPool,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipSESConfigurationSetUsesDedicatedIPPool + ":" + poolName,
	}
}

// eventDestinationSNSTopicRelationship records a configuration-set event
// destination's reported Amazon SNS topic target. SES reports the topic ARN,
// which matches how the SNS scanner publishes its topic resource_id (the topic
// ARN), so the edge joins the topic node. It returns nil when the destination
// has no SNS target.
func eventDestinationSNSTopicRelationship(
	boundary aws.Boundary,
	configurationSet string,
	destination EventDestination,
) *aws.RelationshipObservation {
	topicARN := strings.TrimSpace(destination.SNSTopicARN)
	if topicARN == "" {
		return nil
	}
	sourceID := eventDestinationResourceID(configurationSet, destination)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSESEventDestinationPublishesToSNSTopic,
		SourceResourceID: sourceID,
		TargetResourceID: topicARN,
		TargetARN:        topicARN,
		TargetType:       aws.ResourceTypeSNSTopic,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipSESEventDestinationPublishesToSNSTopic + ":" + topicARN,
	}
}

// eventDestinationFirehoseRelationship records a configuration-set event
// destination's reported Amazon Data Firehose delivery stream target. SES
// reports the delivery stream ARN, which matches how the Firehose scanner
// publishes its delivery-stream resource_id (the stream ARN), so the edge joins
// the stream node. The IAM role ARN SES assumes is recorded only as an edge
// attribute, not as a separate dangling edge. It returns nil when the
// destination has no Firehose target.
func eventDestinationFirehoseRelationship(
	boundary aws.Boundary,
	configurationSet string,
	destination EventDestination,
) *aws.RelationshipObservation {
	streamARN := strings.TrimSpace(destination.FirehoseDeliveryStreamARN)
	if streamARN == "" {
		return nil
	}
	sourceID := eventDestinationResourceID(configurationSet, destination)
	if sourceID == "" {
		return nil
	}
	var attributes map[string]any
	if roleARN := strings.TrimSpace(destination.FirehoseIAMRoleARN); roleARN != "" {
		attributes = map[string]any{"iam_role_arn": roleARN}
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSESEventDestinationStreamsToFirehose,
		SourceResourceID: sourceID,
		TargetResourceID: streamARN,
		TargetARN:        streamARN,
		TargetType:       aws.ResourceTypeFirehoseDeliveryStream,
		Attributes:       attributes,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipSESEventDestinationStreamsToFirehose + ":" + streamARN,
	}
}
