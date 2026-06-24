// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssesv2 "github.com/aws/aws-sdk-go-v2/service/sesv2"
	awssesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	sesservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ses"
)

// mapIdentityInfo maps a ListEmailIdentities summary into the scanner-owned
// identity, populating the name, type, verification status, and sending-enabled
// fields available before GetEmailIdentity enrichment.
func mapIdentityInfo(info awssesv2types.IdentityInfo) sesservice.EmailIdentity {
	return sesservice.EmailIdentity{
		Name:               strings.TrimSpace(aws.ToString(info.IdentityName)),
		IdentityType:       strings.TrimSpace(string(info.IdentityType)),
		VerificationStatus: strings.TrimSpace(string(info.VerificationStatus)),
		SendingEnabled:     info.SendingEnabled,
	}
}

// applyIdentityDetail copies the safe control-plane fields from a
// GetEmailIdentity response onto the scanner-owned identity. It records DKIM
// enabled/status/origin enums but never the DKIM tokens (signing CNAME tokens)
// the SDK also returns, never the identity policy documents in output.Policies,
// and never any signing-key material.
func applyIdentityDetail(identity *sesservice.EmailIdentity, output *awssesv2.GetEmailIdentityOutput) {
	if detailType := strings.TrimSpace(string(output.IdentityType)); detailType != "" {
		identity.IdentityType = detailType
	}
	if status := strings.TrimSpace(string(output.VerificationStatus)); status != "" {
		identity.VerificationStatus = status
	}
	identity.VerifiedForSendingStatus = output.VerifiedForSendingStatus
	identity.FeedbackForwardingStatus = output.FeedbackForwardingStatus
	identity.ConfigurationSetName = strings.TrimSpace(aws.ToString(output.ConfigurationSetName))
	if dkim := output.DkimAttributes; dkim != nil {
		identity.DKIMEnabled = dkim.SigningEnabled
		identity.DKIMStatus = strings.TrimSpace(string(dkim.Status))
		identity.DKIMSigningAttributesOrigin = strings.TrimSpace(string(dkim.SigningAttributesOrigin))
	}
	if mailFrom := output.MailFromAttributes; mailFrom != nil {
		identity.MailFromDomain = strings.TrimSpace(aws.ToString(mailFrom.MailFromDomain))
		identity.MailFromDomainStatus = strings.TrimSpace(string(mailFrom.MailFromDomainStatus))
		identity.MailFromBehaviorOnMxFailure = strings.TrimSpace(string(mailFrom.BehaviorOnMxFailure))
	}
	identity.Tags = tagsToMap(output.Tags)
}

// applyConfigurationSetDetail copies the safe configuration-set options onto the
// scanner-owned set: sending, reputation, delivery (TLS policy and sending
// pool), tracking (custom redirect domain), and tags.
func applyConfigurationSetDetail(set *sesservice.ConfigurationSet, output *awssesv2.GetConfigurationSetOutput) {
	if name := strings.TrimSpace(aws.ToString(output.ConfigurationSetName)); name != "" {
		set.Name = name
	}
	if sending := output.SendingOptions; sending != nil {
		set.SendingEnabled = sending.SendingEnabled
	}
	if reputation := output.ReputationOptions; reputation != nil {
		set.ReputationMetricsEnabled = reputation.ReputationMetricsEnabled
	}
	if delivery := output.DeliveryOptions; delivery != nil {
		set.TLSPolicy = strings.TrimSpace(string(delivery.TlsPolicy))
		set.SendingPoolName = strings.TrimSpace(aws.ToString(delivery.SendingPoolName))
	}
	if tracking := output.TrackingOptions; tracking != nil {
		set.CustomRedirectDomain = strings.TrimSpace(aws.ToString(tracking.CustomRedirectDomain))
	}
	set.Tags = tagsToMap(output.Tags)
}

// mapEventDestination maps an SES event destination into the scanner-owned model
// carrying the destination class and resolvable target references only. It never
// records destination secrets, HEC tokens, or access keys (SES v2 does not
// surface them, and the model has no field for them).
func mapEventDestination(destination awssesv2types.EventDestination) sesservice.EventDestination {
	mapped := sesservice.EventDestination{
		Name:               strings.TrimSpace(aws.ToString(destination.Name)),
		Enabled:            destination.Enabled,
		MatchingEventTypes: eventTypeNames(destination.MatchingEventTypes),
	}
	if sns := destination.SnsDestination; sns != nil {
		mapped.SNSTopicARN = strings.TrimSpace(aws.ToString(sns.TopicArn))
	}
	if firehose := destination.KinesisFirehoseDestination; firehose != nil {
		mapped.FirehoseDeliveryStreamARN = strings.TrimSpace(aws.ToString(firehose.DeliveryStreamArn))
		mapped.FirehoseIAMRoleARN = strings.TrimSpace(aws.ToString(firehose.IamRoleArn))
	}
	if bridge := destination.EventBridgeDestination; bridge != nil {
		mapped.EventBridgeBusARN = strings.TrimSpace(aws.ToString(bridge.EventBusArn))
	}
	if destination.CloudWatchDestination != nil {
		mapped.CloudWatchEnabled = true
	}
	if pinpoint := destination.PinpointDestination; pinpoint != nil {
		mapped.PinpointApplicationARN = strings.TrimSpace(aws.ToString(pinpoint.ApplicationArn))
	}
	return mapped
}

// eventTypeNames returns the trimmed SES event-type enum names, or nil when the
// destination matches no event types.
func eventTypeNames(types []awssesv2types.EventType) []string {
	if len(types) == 0 {
		return nil
	}
	names := make([]string, 0, len(types))
	for _, eventType := range types {
		if name := strings.TrimSpace(string(eventType)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// tagsToMap converts SES resource tags into a trimmed-key string map, or nil
// when there are no usable tags.
func tagsToMap(tags []awssesv2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
