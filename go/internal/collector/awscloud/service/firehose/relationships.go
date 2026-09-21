// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firehose

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// deliveryStreamRelationships builds every graph-join edge a single Firehose
// delivery stream reports: the Kinesis data stream source, the server-side
// encryption KMS key, and per destination the S3 bucket, Redshift cluster,
// OpenSearch domain, delivery IAM role, CloudWatch log group, and transform
// Lambda functions. Each edge emits only when AWS reports the target identity
// in a join-resolvable shape; otherwise the edge is skipped rather than
// dangled. Duplicate role, KMS, log-group, and Lambda targets within one stream
// collapse to a single edge.
func deliveryStreamRelationships(boundary awscloud.Boundary, stream DeliveryStream) []awscloud.RelationshipObservation {
	streamARN := strings.TrimSpace(stream.ARN)
	streamID := firstNonEmpty(streamARN, stream.Name)
	if streamID == "" {
		return nil
	}

	var observations []awscloud.RelationshipObservation

	if edge, ok := sourceKinesisStreamRelationship(boundary, streamID, streamARN, stream); ok {
		observations = append(observations, edge)
	}
	if edge, ok := encryptionKMSKeyRelationship(boundary, streamID, streamARN, stream); ok {
		observations = append(observations, edge)
	}

	seenRole := make(map[string]struct{})
	seenLambda := make(map[string]struct{})
	seenLogGroup := make(map[string]struct{})
	seenS3 := make(map[string]struct{})
	for _, destination := range stream.Destinations {
		if edge, ok := deliveryRoleRelationship(boundary, streamID, streamARN, destination, seenRole); ok {
			observations = append(observations, edge)
		}
		if edge, ok := logGroupRelationship(boundary, streamID, streamARN, destination, seenLogGroup); ok {
			observations = append(observations, edge)
		}
		observations = append(observations,
			transformLambdaRelationships(boundary, streamID, streamARN, destination, seenLambda)...)
		if edge, ok := destinationTargetRelationship(boundary, streamID, streamARN, destination); ok {
			observations = append(observations, edge)
		}
		// An S3-kind destination already emitted its primary bucket edge above;
		// record it so a staging edge to the same bucket does not duplicate.
		if destination.Kind == destinationKindS3 {
			if bucketARN := strings.TrimSpace(destination.S3BucketARN); isARN(bucketARN) {
				seenS3[bucketARN] = struct{}{}
			}
		}
		if edge, ok := stagingS3Relationship(boundary, streamID, streamARN, destination, seenS3); ok {
			observations = append(observations, edge)
		}
	}
	return observations
}

// stagingS3Relationship emits the stream->S3 edge for a non-S3 destination's
// staging/backup bucket. Redshift, Splunk, and HTTP-endpoint destinations stage
// records in S3 around delivery and AWS reports that bucket ARN; without this
// the staging-bucket join the docs advertise is silently dropped. S3 destinations
// emit their primary bucket edge via destinationTargetRelationship, so this skips
// them and dedups any bucket already keyed for the stream.
func stagingS3Relationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	destination Destination,
	seen map[string]struct{},
) (awscloud.RelationshipObservation, bool) {
	if destination.Kind == destinationKindS3 {
		return awscloud.RelationshipObservation{}, false
	}
	bucketARN := strings.TrimSpace(destination.S3BucketARN)
	if !isARN(bucketARN) {
		return awscloud.RelationshipObservation{}, false
	}
	if _, ok := seen[bucketARN]; ok {
		return awscloud.RelationshipObservation{}, false
	}
	seen[bucketARN] = struct{}{}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFirehoseStreamDeliversToS3Bucket,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       map[string]any{"staging_bucket": true},
		SourceRecordID:   streamID + "#s3-staging#" + bucketARN,
	}, true
}

// sourceKinesisStreamRelationship emits the stream-sourced-from-Kinesis edge
// when the delivery stream reads from a Kinesis data stream. AWS reports the
// source stream ARN, and the kinesis scanner publishes its data stream
// resource_id as that ARN, so the edge is ARN-keyed.
func sourceKinesisStreamRelationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	stream DeliveryStream,
) (awscloud.RelationshipObservation, bool) {
	sourceARN := strings.TrimSpace(stream.SourceKinesisStreamARN)
	if !isARN(sourceARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFirehoseStreamSourcedFromKinesisStream,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
		TargetResourceID: sourceARN,
		TargetARN:        sourceARN,
		TargetType:       awscloud.ResourceTypeKinesisDataStream,
		SourceRecordID:   streamID + "#kinesis-source#" + sourceARN,
	}, true
}

// encryptionKMSKeyRelationship emits the stream-uses-KMS-key edge when the
// delivery stream is encrypted with a customer-managed KMS key. AWS reports the
// key ARN, and the kms scanner keys its key node by id-or-ARN, so the edge is
// ARN-keyed.
func encryptionKMSKeyRelationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	stream DeliveryStream,
) (awscloud.RelationshipObservation, bool) {
	keyARN := strings.TrimSpace(stream.EncryptionKMSKeyARN)
	if !isARN(keyARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFirehoseStreamUsesKMSKey,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
		TargetResourceID: keyARN,
		TargetARN:        keyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   streamID + "#kms-key#" + keyARN,
	}, true
}

// deliveryRoleRelationship emits the stream-uses-IAM-role edge for one
// destination's delivery role. AWS reports the role ARN, and the iam scanner
// keys its role node by ARN, so the edge is ARN-keyed. Duplicate roles across a
// stream's destinations collapse via seen.
func deliveryRoleRelationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	destination Destination,
	seen map[string]struct{},
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(destination.RoleARN)
	if !isARN(roleARN) {
		return awscloud.RelationshipObservation{}, false
	}
	if _, ok := seen[roleARN]; ok {
		return awscloud.RelationshipObservation{}, false
	}
	seen[roleARN] = struct{}{}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFirehoseStreamUsesIAMRole,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   streamID + "#role#" + roleARN,
	}, true
}

// logGroupRelationship emits the stream-logs-to-CloudWatch-log-group edge for
// one destination's delivery error logging. AWS reports the log group name, and
// the cloudwatchlogs scanner keys its log group node by ARN-or-name, so the
// edge is name-keyed without a fabricated ARN. Duplicate log groups across a
// stream's destinations collapse via seen.
func logGroupRelationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	destination Destination,
	seen map[string]struct{},
) (awscloud.RelationshipObservation, bool) {
	logGroupName := strings.TrimSpace(destination.LogGroupName)
	if logGroupName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	if _, ok := seen[logGroupName]; ok {
		return awscloud.RelationshipObservation{}, false
	}
	seen[logGroupName] = struct{}{}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFirehoseStreamLogsToCloudWatchLogGroup,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
		TargetResourceID: logGroupName,
		TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
		SourceRecordID:   streamID + "#log-group#" + logGroupName,
	}, true
}

// transformLambdaRelationships emits the stream-uses-Lambda-transform edges for
// one destination's processing configuration. AWS reports each transform Lambda
// ARN, and the lambda scanner keys its function node by ARN, so the edges are
// ARN-keyed. Duplicate Lambda ARNs across a stream's destinations collapse via
// seen.
func transformLambdaRelationships(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	destination Destination,
	seen map[string]struct{},
) []awscloud.RelationshipObservation {
	if len(destination.TransformLambdaARNs) == 0 {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, lambdaARN := range destination.TransformLambdaARNs {
		lambdaARN = strings.TrimSpace(lambdaARN)
		if !isARN(lambdaARN) {
			continue
		}
		if _, ok := seen[lambdaARN]; ok {
			continue
		}
		seen[lambdaARN] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFirehoseStreamUsesLambdaTransform,
			SourceResourceID: streamID,
			SourceARN:        streamARN,
			TargetResourceID: lambdaARN,
			TargetARN:        lambdaARN,
			TargetType:       awscloud.ResourceTypeLambdaFunction,
			SourceRecordID:   streamID + "#lambda-transform#" + lambdaARN,
		})
	}
	return observations
}

// destinationTargetRelationship builds the destination delivery edge for one
// Firehose destination, keyed by the most specific reachable target identity
// for the destination kind. It emits at most one edge per destination. S3 and
// OpenSearch destinations are ARN-keyed (AWS reports the bucket / domain ARN);
// Redshift is keyed by the cluster identifier the Redshift scanner publishes as
// its resource Name. Splunk and HTTP endpoint destinations carry secret-bearing
// access material and report no Eshu-resolvable resource family, so they
// produce no edge here.
func destinationTargetRelationship(
	boundary awscloud.Boundary,
	streamID string,
	streamARN string,
	destination Destination,
) (awscloud.RelationshipObservation, bool) {
	base := awscloud.RelationshipObservation{
		Boundary:         boundary,
		SourceResourceID: streamID,
		SourceARN:        streamARN,
	}
	switch destination.Kind {
	case destinationKindS3:
		bucketARN := strings.TrimSpace(destination.S3BucketARN)
		if !isARN(bucketARN) {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseStreamDeliversToS3Bucket
		base.TargetResourceID = bucketARN
		base.TargetARN = bucketARN
		base.TargetType = awscloud.ResourceTypeS3Bucket
		base.SourceRecordID = streamID + "#s3#" + bucketARN
		return base, true
	case destinationKindRedshift:
		clusterID := strings.TrimSpace(destination.RedshiftClusterIdentifier)
		if clusterID == "" {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseStreamDeliversToRedshiftCluster
		base.TargetResourceID = clusterID
		base.TargetType = awscloud.ResourceTypeRedshiftCluster
		base.SourceRecordID = streamID + "#redshift#" + clusterID
		return base, true
	case destinationKindOpenSearch:
		domainARN := strings.TrimSpace(destination.OpenSearchDomainARN)
		if !isARN(domainARN) {
			return awscloud.RelationshipObservation{}, false
		}
		base.RelationshipType = awscloud.RelationshipFirehoseStreamDeliversToOpenSearchDomain
		base.TargetResourceID = domainARN
		base.TargetARN = domainARN
		base.TargetType = awscloud.ResourceTypeOpenSearchDomain
		base.SourceRecordID = streamID + "#opensearch#" + domainARN
		return base, true
	default:
		return awscloud.RelationshipObservation{}, false
	}
}
