// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesis

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func dataStreamRelationships(boundary awscloud.Boundary, stream DataStream) []awscloud.RelationshipObservation {
	streamID := firstNonEmpty(stream.ARN, stream.Name)
	if streamID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if kmsARN := strings.TrimSpace(stream.KMSKeyID); isARN(kmsARN) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipKinesisDataStreamUsesKMSKey,
			SourceResourceID: streamID,
			SourceARN:        strings.TrimSpace(stream.ARN),
			TargetResourceID: kmsARN,
			TargetARN:        kmsARN,
			TargetType:       awscloud.ResourceTypeKMSKey,
			SourceRecordID:   streamID + "#kms-key#" + kmsARN,
		})
	}
	return observations
}

func videoStreamRelationships(boundary awscloud.Boundary, stream VideoStream) []awscloud.RelationshipObservation {
	streamID := firstNonEmpty(stream.ARN, stream.Name)
	if streamID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if kmsARN := strings.TrimSpace(stream.KMSKeyID); isARN(kmsARN) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipKinesisVideoStreamUsesKMSKey,
			SourceResourceID: streamID,
			SourceARN:        strings.TrimSpace(stream.ARN),
			TargetResourceID: kmsARN,
			TargetARN:        kmsARN,
			TargetType:       awscloud.ResourceTypeKMSKey,
			SourceRecordID:   streamID + "#kms-key#" + kmsARN,
		})
	}
	return observations
}

func deliveryStreamRelationships(boundary awscloud.Boundary, stream FirehoseDeliveryStream) []awscloud.RelationshipObservation {
	streamID := firstNonEmpty(stream.ARN, stream.Name)
	if streamID == "" {
		return nil
	}
	streamARN := strings.TrimSpace(stream.ARN)
	var observations []awscloud.RelationshipObservation
	seenRole := make(map[string]struct{})
	seenLambda := make(map[string]struct{})
	for _, destination := range stream.Destinations {
		if roleARN := strings.TrimSpace(destination.RoleARN); isARN(roleARN) {
			if _, ok := seenRole[roleARN]; !ok {
				seenRole[roleARN] = struct{}{}
				observations = append(observations, awscloud.RelationshipObservation{
					Boundary:         boundary,
					RelationshipType: awscloud.RelationshipFirehoseDeliveryStreamUsesIAMRole,
					SourceResourceID: streamID,
					SourceARN:        streamARN,
					TargetResourceID: roleARN,
					TargetARN:        roleARN,
					TargetType:       awscloud.ResourceTypeIAMRole,
					SourceRecordID:   streamID + "#role#" + roleARN,
				})
			}
		}
		for _, lambdaARN := range destination.TransformLambdaARNs {
			lambdaARN = strings.TrimSpace(lambdaARN)
			if !isARN(lambdaARN) {
				continue
			}
			if _, ok := seenLambda[lambdaARN]; ok {
				continue
			}
			seenLambda[lambdaARN] = struct{}{}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipFirehoseDeliveryStreamUsesLambdaTransform,
				SourceResourceID: streamID,
				SourceARN:        streamARN,
				TargetResourceID: lambdaARN,
				TargetARN:        lambdaARN,
				TargetType:       awscloud.ResourceTypeLambdaFunction,
				SourceRecordID:   streamID + "#lambda-transform#" + lambdaARN,
			})
		}
		if observation, ok := destinationTargetRelationship(boundary, streamID, streamARN, destination); ok {
			observations = append(observations, observation)
		}
	}
	return observations
}

// destinationTargetRelationship builds the destination relationship for one
// Firehose destination. It emits at most one edge per destination, keyed by the
// most specific reachable target identity for the destination kind. Endpoints
// that report no usable target identity produce no edge.
func destinationTargetRelationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	destination FirehoseDestination,
) (awscloud.RelationshipObservation, bool) {
	base := awscloud.RelationshipObservation{
		Boundary:         boundary,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
	}
	switch destination.Kind {
	case FirehoseDestinationKindS3:
		bucketARN := strings.TrimSpace(destination.S3BucketARN)
		if !isARN(bucketARN) {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseDeliveryStreamDeliversToS3
		base.TargetResourceID = bucketARN
		base.TargetARN = bucketARN
		base.TargetType = awscloud.ResourceTypeS3Bucket
		base.SourceRecordID = streamID + "#s3#" + bucketARN
		return base, true
	case FirehoseDestinationKindRedshift:
		clusterID := strings.TrimSpace(destination.RedshiftClusterID)
		if clusterID == "" {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseDeliveryStreamDeliversToRedshift
		base.TargetResourceID = clusterID
		base.TargetType = awscloud.ResourceTypeRedshiftCluster
		base.SourceRecordID = streamID + "#redshift#" + clusterID
		return base, true
	case FirehoseDestinationKindOpenSearch:
		domainARN := strings.TrimSpace(destination.OpenSearchDomainARN)
		if !isARN(domainARN) {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseDeliveryStreamDeliversToOpenSearch
		base.TargetResourceID = domainARN
		base.TargetARN = domainARN
		base.TargetType = awscloud.ResourceTypeOpenSearchDomain
		base.SourceRecordID = streamID + "#opensearch#" + domainARN
		return base, true
	case FirehoseDestinationKindSplunk:
		endpoint := strings.TrimSpace(destination.SplunkEndpoint)
		if endpoint == "" {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseDeliveryStreamDeliversToSplunk
		base.TargetResourceID = endpoint
		base.TargetType = awscloud.ResourceTypeSplunkEndpoint
		base.SourceRecordID = streamID + "#splunk#" + endpoint
		return base, true
	case FirehoseDestinationKindHTTPEndpoint:
		endpoint := firstNonEmpty(destination.HTTPEndpointURL, destination.HTTPEndpointName)
		if endpoint == "" {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint
		base.TargetResourceID = endpoint
		base.TargetType = awscloud.ResourceTypeFirehoseHTTPEndpoint
		base.SourceRecordID = streamID + "#http-endpoint#" + endpoint
		if name := strings.TrimSpace(destination.HTTPEndpointName); name != "" {
			base.Attributes = map[string]any{"endpoint_name": name}
		}
		return base, true
	default:
		return awscloud.RelationshipObservation{}, false
	}
}
