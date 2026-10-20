// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firehose

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
)

// TestDeliveryStreamRelationshipsArePartitionFaithfulAcrossRegions pins the
// GovCloud/China graph-join contract. Firehose reports its destination, role,
// KMS, OpenSearch, and source identities as full ARNs, so the scanner must use
// the reported ARN verbatim rather than synthesizing one with a hardcoded
// commercial partition. This test scans the same stream under commercial,
// GovCloud, and China boundaries and asserts every ARN-keyed edge carries the
// partition AWS reported, so the edges resolve to the partition-correct target
// nodes the S3, IAM, KMS, OpenSearch, and Kinesis scanners publish instead of
// dangling.
func TestDeliveryStreamRelationshipsArePartitionFaithfulAcrossRegions(t *testing.T) {
	cases := []struct {
		name      string
		region    string
		streamARN string
		bucketARN string
		roleARN   string
		kmsKeyARN string
		domainARN string
		sourceARN string
		lambdaARN string
	}{
		{
			name:      "commercial",
			region:    "us-east-1",
			streamARN: "arn:aws:firehose:us-east-1:123456789012:deliverystream/orders",
			bucketARN: "arn:aws:s3:::orders-dest",
			roleARN:   "arn:aws:iam::123456789012:role/firehose",
			kmsKeyARN: "arn:aws:kms:us-east-1:123456789012:key/abc",
			domainARN: "arn:aws:es:us-east-1:123456789012:domain/orders",
			sourceARN: "arn:aws:kinesis:us-east-1:123456789012:stream/raw",
			lambdaARN: "arn:aws:lambda:us-east-1:123456789012:function:t",
		},
		{
			name:      "govcloud",
			region:    "us-gov-west-1",
			streamARN: "arn:aws-us-gov:firehose:us-gov-west-1:123456789012:deliverystream/orders",
			bucketARN: "arn:aws-us-gov:s3:::orders-dest",
			roleARN:   "arn:aws-us-gov:iam::123456789012:role/firehose",
			kmsKeyARN: "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/abc",
			domainARN: "arn:aws-us-gov:es:us-gov-west-1:123456789012:domain/orders",
			sourceARN: "arn:aws-us-gov:kinesis:us-gov-west-1:123456789012:stream/raw",
			lambdaARN: "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:t",
		},
		{
			name:      "china",
			region:    "cn-north-1",
			streamARN: "arn:aws-cn:firehose:cn-north-1:123456789012:deliverystream/orders",
			bucketARN: "arn:aws-cn:s3:::orders-dest",
			roleARN:   "arn:aws-cn:iam::123456789012:role/firehose",
			kmsKeyARN: "arn:aws-cn:kms:cn-north-1:123456789012:key/abc",
			domainARN: "arn:aws-cn:es:cn-north-1:123456789012:domain/orders",
			sourceARN: "arn:aws-cn:kinesis:cn-north-1:123456789012:stream/raw",
			lambdaARN: "arn:aws-cn:lambda:cn-north-1:123456789012:function:t",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region, ServiceKind: awscloud.ServiceFirehose}
			stream := DeliveryStream{
				Name:                   "orders",
				ARN:                    tc.streamARN,
				SourceKinesisStreamARN: tc.sourceARN,
				EncryptionKMSKeyARN:    tc.kmsKeyARN,
				Destinations: []Destination{{
					Kind:                destinationKindOpenSearch,
					RoleARN:             tc.roleARN,
					OpenSearchDomainARN: tc.domainARN,
					TransformLambdaARNs: []string{tc.lambdaARN},
				}, {
					Kind:        destinationKindS3,
					RoleARN:     tc.roleARN,
					S3BucketARN: tc.bucketARN,
				}},
			}

			observations := deliveryStreamRelationships(boundary, stream)
			relguard.AssertObservations(t, observations...)

			want := map[string]string{
				awscloud.RelationshipFirehoseStreamSourcedFromKinesisStream:   tc.sourceARN,
				awscloud.RelationshipFirehoseStreamUsesKMSKey:                 tc.kmsKeyARN,
				awscloud.RelationshipFirehoseStreamUsesIAMRole:                tc.roleARN,
				awscloud.RelationshipFirehoseStreamUsesLambdaTransform:        tc.lambdaARN,
				awscloud.RelationshipFirehoseStreamDeliversToOpenSearchDomain: tc.domainARN,
				awscloud.RelationshipFirehoseStreamDeliversToS3Bucket:         tc.bucketARN,
			}
			seen := make(map[string]string, len(observations))
			for _, obs := range observations {
				seen[obs.RelationshipType] = obs.TargetResourceID
			}
			for relationshipType, wantARN := range want {
				got, ok := seen[relationshipType]
				if !ok {
					t.Fatalf("missing relationship %q for region %q", relationshipType, tc.region)
				}
				if got != wantARN {
					t.Fatalf("relationship %q target_resource_id = %q, want %q (reported ARN, partition-faithful)",
						relationshipType, got, wantARN)
				}
			}
		})
	}
}

// TestDeliveryStreamRelationshipsCollapseDuplicateTargets proves that a role,
// KMS key, log group, or transform Lambda reported on more than one destination
// of the same stream collapses to one edge, so the graph does not carry
// redundant parallel edges for one logical dependency.
func TestDeliveryStreamRelationshipsCollapseDuplicateTargets(t *testing.T) {
	boundary := awscloud.Boundary{Region: "us-east-1", ServiceKind: awscloud.ServiceFirehose}
	roleARN := "arn:aws:iam::123456789012:role/firehose"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:t"
	logGroupName := "/aws/kinesisfirehose/dup"
	stream := DeliveryStream{
		Name: "dup",
		ARN:  "arn:aws:firehose:us-east-1:123456789012:deliverystream/dup",
		Destinations: []Destination{
			{Kind: destinationKindS3, RoleARN: roleARN, S3BucketARN: "arn:aws:s3:::a", LogGroupName: logGroupName, TransformLambdaARNs: []string{lambdaARN}},
			{Kind: destinationKindS3, RoleARN: roleARN, S3BucketARN: "arn:aws:s3:::b", LogGroupName: logGroupName, TransformLambdaARNs: []string{lambdaARN}},
		},
	}

	observations := deliveryStreamRelationships(boundary, stream)
	counts := map[string]int{}
	for _, obs := range observations {
		counts[obs.RelationshipType]++
	}
	if got := counts[awscloud.RelationshipFirehoseStreamUsesIAMRole]; got != 1 {
		t.Fatalf("role edge count = %d, want 1", got)
	}
	if got := counts[awscloud.RelationshipFirehoseStreamUsesLambdaTransform]; got != 1 {
		t.Fatalf("lambda edge count = %d, want 1", got)
	}
	if got := counts[awscloud.RelationshipFirehoseStreamLogsToCloudWatchLogGroup]; got != 1 {
		t.Fatalf("log-group edge count = %d, want 1", got)
	}
	if got := counts[awscloud.RelationshipFirehoseStreamDeliversToS3Bucket]; got != 2 {
		t.Fatalf("s3 edge count = %d, want 2 (distinct buckets stay distinct)", got)
	}
}
