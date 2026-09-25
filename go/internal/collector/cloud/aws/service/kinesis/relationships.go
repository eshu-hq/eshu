// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesis

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func dataStreamRelationships(boundary aws.Boundary, stream DataStream) []aws.RelationshipObservation {
	streamID := firstNonEmpty(stream.ARN, stream.Name)
	if streamID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if kmsARN := strings.TrimSpace(stream.KMSKeyID); isARN(kmsARN) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipKinesisDataStreamUsesKMSKey,
			SourceResourceID: streamID,
			SourceARN:        strings.TrimSpace(stream.ARN),
			TargetResourceID: kmsARN,
			TargetARN:        kmsARN,
			TargetType:       aws.ResourceTypeKMSKey,
			SourceRecordID:   streamID + "#kms-key#" + kmsARN,
		})
	}
	return observations
}

func videoStreamRelationships(boundary aws.Boundary, stream VideoStream) []aws.RelationshipObservation {
	streamID := firstNonEmpty(stream.ARN, stream.Name)
	if streamID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if kmsARN := strings.TrimSpace(stream.KMSKeyID); isARN(kmsARN) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipKinesisVideoStreamUsesKMSKey,
			SourceResourceID: streamID,
			SourceARN:        strings.TrimSpace(stream.ARN),
			TargetResourceID: kmsARN,
			TargetARN:        kmsARN,
			TargetType:       aws.ResourceTypeKMSKey,
			SourceRecordID:   streamID + "#kms-key#" + kmsARN,
		})
	}
	return observations
}

func deliveryStreamRelationships(boundary aws.Boundary, stream FirehoseDeliveryStream) []aws.RelationshipObservation {
	streamID := firstNonEmpty(stream.ARN, stream.Name)
	if streamID == "" {
		return nil
	}
	streamARN := strings.TrimSpace(stream.ARN)
	var observations []aws.RelationshipObservation
	seenRole := make(map[string]struct{})
	seenLambda := make(map[string]struct{})
	for _, destination := range stream.Destinations {
		if roleARN := strings.TrimSpace(destination.RoleARN); isARN(roleARN) {
			if _, ok := seenRole[roleARN]; !ok {
				seenRole[roleARN] = struct{}{}
				observations = append(observations, aws.RelationshipObservation{
					Boundary:         boundary,
					RelationshipType: aws.RelationshipFirehoseDeliveryStreamUsesIAMRole,
					SourceResourceID: streamID,
					SourceARN:        streamARN,
					TargetResourceID: roleARN,
					TargetARN:        roleARN,
					TargetType:       aws.ResourceTypeIAMRole,
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
			observations = append(observations, aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipFirehoseDeliveryStreamUsesLambdaTransform,
				SourceResourceID: streamID,
				SourceARN:        streamARN,
				TargetResourceID: lambdaARN,
				TargetARN:        lambdaARN,
				TargetType:       aws.ResourceTypeLambdaFunction,
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
	boundary aws.Boundary,
	streamID string,
	streamARN string,
	destination FirehoseDestination,
) (aws.RelationshipObservation, bool) {
	base := aws.RelationshipObservation{
		Boundary:         boundary,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
	}
	switch destination.Kind {
	case FirehoseDestinationKindS3:
		bucketARN := strings.TrimSpace(destination.S3BucketARN)
		if !isARN(bucketARN) {
			return aws.RelationshipObservation{}, false
		}
		base.RelationshipType = aws.RelationshipFirehoseDeliveryStreamDeliversToS3
		base.TargetResourceID = bucketARN
		base.TargetARN = bucketARN
		base.TargetType = aws.ResourceTypeS3Bucket
		base.SourceRecordID = streamID + "#s3#" + bucketARN
		return base, true
	case FirehoseDestinationKindRedshift:
		clusterID := strings.TrimSpace(destination.RedshiftClusterID)
		if clusterID == "" {
			return aws.RelationshipObservation{}, false
		}
		base.RelationshipType = aws.RelationshipFirehoseDeliveryStreamDeliversToRedshift
		base.TargetResourceID = clusterID
		base.TargetType = aws.ResourceTypeRedshiftCluster
		base.SourceRecordID = streamID + "#redshift#" + clusterID
		return base, true
	case FirehoseDestinationKindOpenSearch:
		domainARN := strings.TrimSpace(destination.OpenSearchDomainARN)
		if !isARN(domainARN) {
			return aws.RelationshipObservation{}, false
		}
		base.RelationshipType = aws.RelationshipFirehoseDeliveryStreamDeliversToOpenSearch
		base.TargetResourceID = domainARN
		base.TargetARN = domainARN
		base.TargetType = aws.ResourceTypeOpenSearchDomain
		base.SourceRecordID = streamID + "#opensearch#" + domainARN
		return base, true
	case FirehoseDestinationKindSplunk:
		endpoint := strings.TrimSpace(destination.SplunkEndpoint)
		if endpoint == "" {
			return aws.RelationshipObservation{}, false
		}
		base.RelationshipType = aws.RelationshipFirehoseDeliveryStreamDeliversToSplunk
		base.TargetResourceID = endpoint
		base.TargetType = aws.ResourceTypeSplunkEndpoint
		base.SourceRecordID = streamID + "#splunk#" + endpoint
		return base, true
	case FirehoseDestinationKindHTTPEndpoint:
		endpoint := firstNonEmpty(destination.HTTPEndpointURL, destination.HTTPEndpointName)
		if endpoint == "" {
			return aws.RelationshipObservation{}, false
		}
		base.RelationshipType = aws.RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint
		base.TargetResourceID = endpoint
		base.TargetType = aws.ResourceTypeFirehoseHTTPEndpoint
		base.SourceRecordID = streamID + "#http-endpoint#" + endpoint
		if name := strings.TrimSpace(destination.HTTPEndpointName); name != "" {
			base.Attributes = map[string]any{"endpoint_name": name}
		}
		return base, true
	default:
		return aws.RelationshipObservation{}, false
	}
}
