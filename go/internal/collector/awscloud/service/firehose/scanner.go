// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firehose

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Data Firehose metadata-only facts for one claimed
// account and region. It never reads delivery records, never mutates a delivery
// stream, and never persists destination access keys, Splunk HEC tokens,
// Redshift passwords, or processing-configuration Lambda bodies.
type Scanner struct {
	// Client is the metadata-only Firehose read surface. It is required.
	Client Client
}

// Scan observes Firehose delivery streams through the configured client and
// emits one resource fact per delivery stream plus relationship facts for the
// stream's S3, Redshift, and OpenSearch destinations, its Kinesis data stream
// source, its delivery IAM role, its server-side encryption KMS key, its
// CloudWatch error-logging log group, and its transform Lambda functions.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("firehose scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceFirehose:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceFirehose
	default:
		return nil, fmt.Errorf("firehose scanner received service_kind %q", boundary.ServiceKind)
	}

	streams, err := s.Client.ListDeliveryStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Firehose delivery streams: %w", err)
	}

	var envelopes []facts.Envelope
	for _, stream := range streams {
		resource, err := awscloud.NewResourceEnvelope(deliveryStreamObservation(boundary, stream))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		relationships, err := relationshipEnvelopes(deliveryStreamRelationships(boundary, stream))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

// relationshipEnvelopes wraps each relationship observation in a fact envelope.
// It returns a nil slice for an empty input so callers append nothing.
func relationshipEnvelopes(observations []awscloud.RelationshipObservation) ([]facts.Envelope, error) {
	if len(observations) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(observations))
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// deliveryStreamObservation maps one Firehose delivery stream into a
// reported-confidence resource observation. The resource id prefers the stream
// ARN and falls back to the stream name. Destination kinds are summarized as a
// presence list; no destination payload is persisted.
func deliveryStreamObservation(boundary awscloud.Boundary, stream DeliveryStream) awscloud.ResourceObservation {
	streamARN := strings.TrimSpace(stream.ARN)
	resourceID := firstNonEmpty(streamARN, stream.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          streamARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeFirehoseDeliveryStream,
		Name:         strings.TrimSpace(stream.Name),
		State:        strings.TrimSpace(stream.Status),
		Tags:         cloneStringMap(stream.Tags),
		Attributes: map[string]any{
			"delivery_stream_type":      strings.TrimSpace(stream.StreamType),
			"source_type":               strings.TrimSpace(stream.SourceType),
			"source_kinesis_stream_arn": strings.TrimSpace(stream.SourceKinesisStreamARN),
			"encryption_mode":           strings.TrimSpace(stream.EncryptionMode),
			"encryption_status":         strings.TrimSpace(stream.EncryptionStatus),
			"encryption_kms_key_arn":    strings.TrimSpace(stream.EncryptionKMSKeyARN),
			"creation_timestamp":        timeOrNil(stream.CreationTimestamp),
			"destination_types":         destinationKinds(stream.Destinations),
		},
		CorrelationAnchors: []string{streamARN, strings.TrimSpace(stream.Name)},
		SourceRecordID:     resourceID,
	}
}

// destinationKinds returns the deduplicated set of destination kinds reported
// for a delivery stream so the resource attribute records destination classes
// without persisting any destination payload.
func destinationKinds(destinations []Destination) []string {
	if len(destinations) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		kinds = append(kinds, destination.Kind)
	}
	return dedupeNonEmpty(kinds)
}
