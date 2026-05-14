package sns

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS SNS topic metadata facts for one claimed account and
// region. It never publishes messages and never persists topic policy JSON.
type Scanner struct {
	Client Client
}

// Scan observes SNS topics through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("sns scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceSNS
	case awscloud.ServiceSNS:
	default:
		return nil, fmt.Errorf("sns scanner received service_kind %q", boundary.ServiceKind)
	}

	topics, err := s.Client.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list SNS topics: %w", err)
	}
	var envelopes []facts.Envelope
	for _, topic := range topics {
		resource, err := awscloud.NewResourceEnvelope(topicObservation(boundary, topic))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, subscription := range topic.Subscriptions {
			relationship, ok := subscriptionRelationship(boundary, topic, subscription)
			if !ok {
				continue
			}
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func topicObservation(boundary awscloud.Boundary, topic Topic) awscloud.ResourceObservation {
	topicARN := strings.TrimSpace(topic.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          topicARN,
		ResourceID:   firstNonEmpty(topicARN, topic.Name),
		ResourceType: awscloud.ResourceTypeSNSTopic,
		Name:         strings.TrimSpace(topic.Name),
		Tags:         cloneStringMap(topic.Tags),
		Attributes: map[string]any{
			"archive_policy":              strings.TrimSpace(topic.Attributes.ArchivePolicy),
			"beginning_archive_time":      strings.TrimSpace(topic.Attributes.BeginningArchiveTime),
			"content_based_deduplication": topic.Attributes.ContentBasedDeduplication,
			"display_name":                strings.TrimSpace(topic.Attributes.DisplayName),
			"fifo_topic":                  topic.Attributes.FIFOTopic,
			"kms_master_key_id":           strings.TrimSpace(topic.Attributes.KMSMasterKeyID),
			"owner":                       strings.TrimSpace(topic.Attributes.Owner),
			"signature_version":           strings.TrimSpace(topic.Attributes.SignatureVersion),
			"subscriptions_confirmed":     strings.TrimSpace(topic.Attributes.SubscriptionsConfirmed),
			"subscriptions_deleted":       strings.TrimSpace(topic.Attributes.SubscriptionsDeleted),
			"subscriptions_pending":       strings.TrimSpace(topic.Attributes.SubscriptionsPending),
			"tracing_config":              strings.TrimSpace(topic.Attributes.TracingConfig),
		},
		CorrelationAnchors: []string{topicARN, topic.Name},
		SourceRecordID:     firstNonEmpty(topicARN, topic.Name),
	}
}

func subscriptionRelationship(
	boundary awscloud.Boundary,
	topic Topic,
	subscription Subscription,
) (awscloud.RelationshipObservation, bool) {
	topicARN := strings.TrimSpace(topic.ARN)
	endpointARN := strings.TrimSpace(subscription.EndpointARN)
	if topicARN == "" || endpointARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSNSTopicDeliversToResource,
		SourceResourceID: firstNonEmpty(topicARN, topic.Name),
		SourceARN:        topicARN,
		TargetResourceID: endpointARN,
		TargetARN:        endpointARN,
		TargetType:       targetTypeForARN(endpointARN),
		Attributes: map[string]any{
			"owner":            strings.TrimSpace(subscription.Owner),
			"protocol":         strings.TrimSpace(subscription.Protocol),
			"subscription_arn": strings.TrimSpace(subscription.SubscriptionARN),
		},
		SourceRecordID: firstNonEmpty(subscription.SubscriptionARN, topicARN+"->"+endpointARN),
	}, true
}

func targetTypeForARN(arn string) string {
	switch {
	case strings.Contains(arn, ":sqs:"):
		return awscloud.ResourceTypeSQSQueue
	case strings.Contains(arn, ":lambda:"):
		return awscloud.ResourceTypeLambdaFunction
	default:
		return "aws_resource"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
