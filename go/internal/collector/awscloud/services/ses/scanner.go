// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ses

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Simple Email Service (SES v2) metadata-only facts for one
// claimed account and region. It reads email-identity, configuration set,
// configuration-set event-destination, and dedicated-IP-pool control-plane
// metadata only. It never sends email, never reads message or template bodies,
// and never persists DKIM private keys, DKIM signing tokens, identity policy
// documents, or SMTP credentials. It reports those resources plus the
// identity-to-default-configuration-set, configuration-set-to-dedicated-IP-pool,
// event-destination-to-SNS-topic, event-destination-to-Firehose-delivery-stream,
// and (defensively) identity-DKIM-to-KMS-key relationships.
type Scanner struct {
	// Client is the metadata-only SES snapshot source.
	Client Client
}

// Scan observes SES email identities, configuration sets and their event
// destinations, dedicated IP pools, and the direct cross-service dependency
// metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ses scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSES:
		boundary.ServiceKind = awscloud.ServiceSES
	default:
		return nil, fmt.Errorf("ses scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot SES metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, identity := range snapshot.EmailIdentities {
		next, err := identityEnvelopes(boundary, identity)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, set := range snapshot.ConfigurationSets {
		next, err := configurationSetEnvelopes(boundary, set)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, pool := range snapshot.DedicatedIPPools {
		envelope, err := awscloud.NewResourceEnvelope(dedicatedIPPoolObservation(boundary, pool))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
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

func identityEnvelopes(boundary awscloud.Boundary, identity EmailIdentity) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(identityObservation(boundary, identity))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		identityConfigurationSetRelationship(boundary, identity),
		identityDKIMKMSRelationship(boundary, identity),
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

func configurationSetEnvelopes(boundary awscloud.Boundary, set ConfigurationSet) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(configurationSetObservation(boundary, set))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := configurationSetDedicatedIPPoolRelationship(boundary, set); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	setID := configurationSetResourceID(set)
	for _, destination := range set.EventDestinations {
		next, err := eventDestinationEnvelopes(boundary, setID, destination)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func eventDestinationEnvelopes(
	boundary awscloud.Boundary,
	configurationSet string,
	destination EventDestination,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(eventDestinationObservation(boundary, configurationSet, destination))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		eventDestinationSNSTopicRelationship(boundary, configurationSet, destination),
		eventDestinationFirehoseRelationship(boundary, configurationSet, destination),
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

func identityObservation(boundary awscloud.Boundary, identity EmailIdentity) awscloud.ResourceObservation {
	name := strings.TrimSpace(identity.Name)
	resourceID := identityResourceID(identity)
	arn := identityARN(boundary, identity)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSESEmailIdentity,
		Name:         name,
		State:        strings.TrimSpace(identity.VerificationStatus),
		Tags:         cloneStringMap(identity.Tags),
		Attributes: map[string]any{
			"identity_type":                  strings.TrimSpace(identity.IdentityType),
			"verification_status":            strings.TrimSpace(identity.VerificationStatus),
			"verified_for_sending_status":    identity.VerifiedForSendingStatus,
			"sending_enabled":                identity.SendingEnabled,
			"feedback_forwarding_status":     identity.FeedbackForwardingStatus,
			"configuration_set_name":         strings.TrimSpace(identity.ConfigurationSetName),
			"dkim_enabled":                   identity.DKIMEnabled,
			"dkim_status":                    strings.TrimSpace(identity.DKIMStatus),
			"dkim_signing_attributes_origin": strings.TrimSpace(identity.DKIMSigningAttributesOrigin),
			"mail_from_domain":               strings.TrimSpace(identity.MailFromDomain),
			"mail_from_domain_status":        strings.TrimSpace(identity.MailFromDomainStatus),
			"mail_from_behavior_on_mx_failure": strings.TrimSpace(
				identity.MailFromBehaviorOnMxFailure,
			),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func configurationSetObservation(boundary awscloud.Boundary, set ConfigurationSet) awscloud.ResourceObservation {
	name := strings.TrimSpace(set.Name)
	resourceID := configurationSetResourceID(set)
	arn := configurationSetARN(boundary, set)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSESConfigurationSet,
		Name:         name,
		Tags:         cloneStringMap(set.Tags),
		Attributes: map[string]any{
			"sending_enabled":            set.SendingEnabled,
			"reputation_metrics_enabled": set.ReputationMetricsEnabled,
			"tls_policy":                 strings.TrimSpace(set.TLSPolicy),
			"sending_pool_name":          strings.TrimSpace(set.SendingPoolName),
			"custom_redirect_domain":     strings.TrimSpace(set.CustomRedirectDomain),
			"event_destination_count":    len(set.EventDestinations),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func eventDestinationObservation(
	boundary awscloud.Boundary,
	configurationSet string,
	destination EventDestination,
) awscloud.ResourceObservation {
	name := strings.TrimSpace(destination.Name)
	resourceID := eventDestinationResourceID(configurationSet, destination)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSESEventDestination,
		Name:         name,
		Attributes: map[string]any{
			"configuration_set_name":       strings.TrimSpace(configurationSet),
			"enabled":                      destination.Enabled,
			"matching_event_types":         cloneStrings(destination.MatchingEventTypes),
			"destination_classes":          destinationClasses(destination),
			"sns_topic_arn":                strings.TrimSpace(destination.SNSTopicARN),
			"firehose_delivery_stream_arn": strings.TrimSpace(destination.FirehoseDeliveryStreamARN),
			"event_bridge_bus_arn":         strings.TrimSpace(destination.EventBridgeBusARN),
			"cloudwatch_enabled":           destination.CloudWatchEnabled,
			"pinpoint_application_arn":     strings.TrimSpace(destination.PinpointApplicationARN),
		},
		CorrelationAnchors: []string{resourceID, name},
		SourceRecordID:     resourceID,
	}
}

func dedicatedIPPoolObservation(boundary awscloud.Boundary, pool DedicatedIPPool) awscloud.ResourceObservation {
	name := strings.TrimSpace(pool.Name)
	resourceID := dedicatedIPPoolResourceID(pool)
	arn := dedicatedIPPoolARN(boundary, pool)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSESDedicatedIPPool,
		Name:         name,
		Attributes: map[string]any{
			"pool_name": name,
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
