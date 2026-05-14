package sqs

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS SQS queue metadata facts for one claimed account and
// region. It never reads messages and never persists queue policy JSON.
type Scanner struct {
	Client Client
}

// Scan observes SQS queues through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("sqs scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceSQS
	case awscloud.ServiceSQS:
	default:
		return nil, fmt.Errorf("sqs scanner received service_kind %q", boundary.ServiceKind)
	}

	queues, err := s.Client.ListQueues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list SQS queues: %w", err)
	}
	var envelopes []facts.Envelope
	for _, queue := range queues {
		resource, err := awscloud.NewResourceEnvelope(queueObservation(boundary, queue))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if relationship, ok := deadLetterQueueRelationship(boundary, queue); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func queueObservation(boundary awscloud.Boundary, queue Queue) awscloud.ResourceObservation {
	queueARN := strings.TrimSpace(queue.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          queueARN,
		ResourceID:   firstNonEmpty(queueARN, queue.URL, queue.Name),
		ResourceType: awscloud.ResourceTypeSQSQueue,
		Name:         strings.TrimSpace(queue.Name),
		Tags:         cloneStringMap(queue.Tags),
		Attributes: map[string]any{
			"queue_url":                         strings.TrimSpace(queue.URL),
			"delay_seconds":                     strings.TrimSpace(queue.Attributes.DelaySeconds),
			"fifo_queue":                        queue.Attributes.FIFOQueue,
			"content_based_deduplication":       queue.Attributes.ContentBasedDeduplication,
			"deduplication_scope":               strings.TrimSpace(queue.Attributes.DeduplicationScope),
			"fifo_throughput_limit":             strings.TrimSpace(queue.Attributes.FIFOThroughputLimit),
			"maximum_message_size":              strings.TrimSpace(queue.Attributes.MaximumMessageSize),
			"message_retention_period":          strings.TrimSpace(queue.Attributes.MessageRetentionPeriod),
			"receive_message_wait_time_seconds": strings.TrimSpace(queue.Attributes.ReceiveMessageWaitTimeSeconds),
			"visibility_timeout":                strings.TrimSpace(queue.Attributes.VisibilityTimeout),
			"kms_master_key_id":                 strings.TrimSpace(queue.Attributes.KMSMasterKeyID),
			"kms_data_key_reuse_period_seconds": strings.TrimSpace(queue.Attributes.KMSDataKeyReusePeriodSeconds),
			"sqs_managed_sse_enabled":           queue.Attributes.SQSManagedSSEEnabled,
			"dead_letter_target_arn":            strings.TrimSpace(queue.Attributes.DeadLetterTargetARN),
			"max_receive_count":                 strings.TrimSpace(queue.Attributes.MaxReceiveCount),
			"redrive_permission":                strings.TrimSpace(queue.Attributes.RedrivePermission),
			"redrive_source_queue_arns":         cloneStrings(queue.Attributes.RedriveSourceQueueARNs),
			"created_timestamp":                 strings.TrimSpace(queue.Attributes.CreatedTimestamp),
			"last_modified_timestamp":           strings.TrimSpace(queue.Attributes.LastModifiedTimestamp),
		},
		CorrelationAnchors: []string{queueARN, queue.URL, queue.Name},
		SourceRecordID:     firstNonEmpty(queueARN, queue.URL, queue.Name),
	}
}

func deadLetterQueueRelationship(
	boundary awscloud.Boundary,
	queue Queue,
) (awscloud.RelationshipObservation, bool) {
	queueARN := strings.TrimSpace(queue.ARN)
	dlqARN := strings.TrimSpace(queue.Attributes.DeadLetterTargetARN)
	if queueARN == "" || dlqARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSQSQueueUsesDeadLetterQueue,
		SourceResourceID: firstNonEmpty(queueARN, queue.URL, queue.Name),
		SourceARN:        queueARN,
		TargetResourceID: dlqARN,
		TargetARN:        dlqARN,
		TargetType:       awscloud.ResourceTypeSQSQueue,
		Attributes: map[string]any{
			"max_receive_count": strings.TrimSpace(queue.Attributes.MaxReceiveCount),
		},
	}, true
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

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
