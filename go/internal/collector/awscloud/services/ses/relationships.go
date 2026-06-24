// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ses

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// identityConfigurationSetRelationship records an SES email identity's reported
// default configuration set. SES reports the configuration set NAME, which is
// the resource_id the SES configuration-set node publishes, so the edge joins
// that node exactly. It returns nil when the identity reports no default set.
func identityConfigurationSetRelationship(
	boundary awscloud.Boundary,
	identity EmailIdentity,
) *awscloud.RelationshipObservation {
	setName := strings.TrimSpace(identity.ConfigurationSetName)
	if setName == "" {
		return nil
	}
	sourceID := identityResourceID(identity)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSESEmailIdentityUsesConfigurationSet,
		SourceResourceID: sourceID,
		SourceARN:        identityARN(boundary, identity),
		TargetResourceID: setName,
		TargetType:       awscloud.ResourceTypeSESConfigurationSet,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipSESEmailIdentityUsesConfigurationSet + ":" + setName,
	}
}

// identityDKIMKMSRelationship records an SES email identity's reported DKIM
// customer KMS key dependency. SES v2 does not surface a customer KMS key on the
// DKIM attributes today, so this edge is emitted only on the defensive path
// where AWS ever reports a key identifier. The target is keyed by the reported
// key identifier the KMS scanner publishes (a key ARN). It returns nil when no
// key identifier is reported.
func identityDKIMKMSRelationship(
	boundary awscloud.Boundary,
	identity EmailIdentity,
) *awscloud.RelationshipObservation {
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
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSESEmailIdentityDKIMUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        identityARN(boundary, identity),
		TargetResourceID: keyID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipSESEmailIdentityDKIMUsesKMSKey + ":" + keyID,
	}
}

// configurationSetDedicatedIPPoolRelationship records a configuration set's
// reported sending (dedicated IP) pool. SES reports the pool NAME, which is the
// resource_id the SES dedicated-IP-pool node publishes, so the edge joins that
// node. It returns nil when the set sends through the shared pool.
func configurationSetDedicatedIPPoolRelationship(
	boundary awscloud.Boundary,
	set ConfigurationSet,
) *awscloud.RelationshipObservation {
	poolName := strings.TrimSpace(set.SendingPoolName)
	if poolName == "" {
		return nil
	}
	sourceID := configurationSetResourceID(set)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSESConfigurationSetUsesDedicatedIPPool,
		SourceResourceID: sourceID,
		SourceARN:        configurationSetARN(boundary, set),
		TargetResourceID: poolName,
		TargetType:       awscloud.ResourceTypeSESDedicatedIPPool,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipSESConfigurationSetUsesDedicatedIPPool + ":" + poolName,
	}
}

// eventDestinationSNSTopicRelationship records a configuration-set event
// destination's reported Amazon SNS topic target. SES reports the topic ARN,
// which matches how the SNS scanner publishes its topic resource_id (the topic
// ARN), so the edge joins the topic node. It returns nil when the destination
// has no SNS target.
func eventDestinationSNSTopicRelationship(
	boundary awscloud.Boundary,
	configurationSet string,
	destination EventDestination,
) *awscloud.RelationshipObservation {
	topicARN := strings.TrimSpace(destination.SNSTopicARN)
	if topicARN == "" {
		return nil
	}
	sourceID := eventDestinationResourceID(configurationSet, destination)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSESEventDestinationPublishesToSNSTopic,
		SourceResourceID: sourceID,
		TargetResourceID: topicARN,
		TargetARN:        topicARN,
		TargetType:       awscloud.ResourceTypeSNSTopic,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipSESEventDestinationPublishesToSNSTopic + ":" + topicARN,
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
	boundary awscloud.Boundary,
	configurationSet string,
	destination EventDestination,
) *awscloud.RelationshipObservation {
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
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSESEventDestinationStreamsToFirehose,
		SourceResourceID: sourceID,
		TargetResourceID: streamARN,
		TargetARN:        streamARN,
		TargetType:       awscloud.ResourceTypeFirehoseDeliveryStream,
		Attributes:       attributes,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipSESEventDestinationStreamsToFirehose + ":" + streamARN,
	}
}
