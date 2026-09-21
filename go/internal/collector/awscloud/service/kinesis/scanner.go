// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Kinesis metadata facts for one claimed account and region.
// It covers Kinesis Data Streams, Kinesis Data Firehose, and Kinesis Video
// Streams. It never reads stream records, never reads video media fragments,
// never mutates any resource, and never persists processing-configuration
// Lambda bodies or destination secret material (HTTP endpoint access keys,
// Splunk HEC tokens, Redshift passwords).
type Scanner struct {
	Client Client
}

// Scan observes Kinesis data streams, Firehose delivery streams, and video
// streams through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("kinesis scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceKinesis:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceKinesis
	default:
		return nil, fmt.Errorf("kinesis scanner received service_kind %q", boundary.ServiceKind)
	}

	dataStreams, err := s.Client.ListDataStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Kinesis data streams: %w", err)
	}
	deliveryStreams, err := s.Client.ListFirehoseDeliveryStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Kinesis Firehose delivery streams: %w", err)
	}
	videoStreams, err := s.Client.ListVideoStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Kinesis video streams: %w", err)
	}

	var envelopes []facts.Envelope
	for _, stream := range dataStreams {
		resource, err := awscloud.NewResourceEnvelope(dataStreamObservation(boundary, stream))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationships, err := relationshipEnvelopes(dataStreamRelationships(boundary, stream))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	for _, stream := range deliveryStreams {
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
	for _, stream := range videoStreams {
		resource, err := awscloud.NewResourceEnvelope(videoStreamObservation(boundary, stream))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationships, err := relationshipEnvelopes(videoStreamRelationships(boundary, stream))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

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

func dataStreamObservation(boundary awscloud.Boundary, stream DataStream) awscloud.ResourceObservation {
	streamARN := strings.TrimSpace(stream.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          streamARN,
		ResourceID:   firstNonEmpty(streamARN, stream.Name),
		ResourceType: awscloud.ResourceTypeKinesisDataStream,
		Name:         strings.TrimSpace(stream.Name),
		State:        strings.TrimSpace(stream.Status),
		Tags:         cloneStringMap(stream.Tags),
		Attributes: map[string]any{
			"stream_mode":            strings.TrimSpace(stream.StreamMode),
			"open_shard_count":       stream.OpenShardCount,
			"retention_period_hours": stream.RetentionHours,
			"encryption_type":        strings.TrimSpace(stream.EncryptionType),
			"kms_key_id":             strings.TrimSpace(stream.KMSKeyID),
			"creation_timestamp":     timeOrNil(stream.CreationTimestamp),
		},
		CorrelationAnchors: []string{streamARN, strings.TrimSpace(stream.Name)},
		SourceRecordID:     firstNonEmpty(streamARN, stream.Name),
	}
}

func deliveryStreamObservation(boundary awscloud.Boundary, stream FirehoseDeliveryStream) awscloud.ResourceObservation {
	streamARN := strings.TrimSpace(stream.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          streamARN,
		ResourceID:   firstNonEmpty(streamARN, stream.Name),
		ResourceType: awscloud.ResourceTypeKinesisFirehoseDeliveryStream,
		Name:         strings.TrimSpace(stream.Name),
		State:        strings.TrimSpace(stream.Status),
		Tags:         cloneStringMap(stream.Tags),
		Attributes: map[string]any{
			"delivery_stream_type":   strings.TrimSpace(stream.StreamType),
			"source_kinesis_stream":  strings.TrimSpace(stream.SourceKinesisStream),
			"encryption_status":      strings.TrimSpace(stream.EncryptionStatus),
			"encryption_key_type":    strings.TrimSpace(stream.EncryptionKeyType),
			"encryption_kms_key_arn": strings.TrimSpace(stream.EncryptionKMSKeyARN),
			"creation_timestamp":     timeOrNil(stream.CreationTimestamp),
			"destination_types":      destinationKinds(stream.Destinations),
		},
		CorrelationAnchors: []string{streamARN, strings.TrimSpace(stream.Name)},
		SourceRecordID:     firstNonEmpty(streamARN, stream.Name),
	}
}

func videoStreamObservation(boundary awscloud.Boundary, stream VideoStream) awscloud.ResourceObservation {
	streamARN := strings.TrimSpace(stream.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          streamARN,
		ResourceID:   firstNonEmpty(streamARN, stream.Name),
		ResourceType: awscloud.ResourceTypeKinesisVideoStream,
		Name:         strings.TrimSpace(stream.Name),
		State:        strings.TrimSpace(stream.Status),
		Tags:         cloneStringMap(stream.Tags),
		Attributes: map[string]any{
			"kms_key_id":           strings.TrimSpace(stream.KMSKeyID),
			"media_type":           strings.TrimSpace(stream.MediaType),
			"data_retention_hours": stream.RetentionHours,
			"creation_timestamp":   timeOrNil(stream.CreationTimestamp),
		},
		CorrelationAnchors: []string{streamARN, strings.TrimSpace(stream.Name)},
		SourceRecordID:     firstNonEmpty(streamARN, stream.Name),
	}
}

// destinationKinds returns the deduplicated set of destination kinds reported
// for a Firehose delivery stream so the resource attribute records destination
// classes without persisting any destination payload.
func destinationKinds(destinations []FirehoseDestination) []string {
	if len(destinations) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(destinations))
	var kinds []string
	for _, destination := range destinations {
		kind := strings.TrimSpace(destination.Kind)
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	return kinds
}
