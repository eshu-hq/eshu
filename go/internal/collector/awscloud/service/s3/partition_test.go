// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestBucketNodeIdentityDerivesPartition pins the keystone contract: the S3
// bucket CloudResource node identity (ARN, ResourceID, and the ARN correlation
// anchor) must carry the partition of the scan boundary's region, not a
// hardcoded commercial `aws`. S3 buckets carry no API ARN, so the scanner
// synthesizes it; a hardcoded partition makes every partition-aware consumer
// (Bedrock, CodePipeline, MQ, Config, SageMaker, Glue, Athena) dangle its S3
// edge in aws-us-gov and aws-cn because the join is ARN-equality.
func TestBucketNodeIdentityDerivesPartition(t *testing.T) {
	cases := []struct {
		name    string
		region  string
		wantARN string
	}{
		{name: "commercial", region: "us-east-1", wantARN: "arn:aws:s3:::my-bucket"},
		{name: "govcloud", region: "us-gov-west-1", wantARN: "arn:aws-us-gov:s3:::my-bucket"},
		{name: "china", region: "cn-north-1", wantARN: "arn:aws-cn:s3:::my-bucket"},
		{name: "blank region falls back to commercial", region: "", wantARN: "arn:aws:s3:::my-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := bucketObservation(awscloud.Boundary{Region: tc.region}, Bucket{Name: "my-bucket"})
			if obs.ARN != tc.wantARN {
				t.Fatalf("node ARN = %q, want %q", obs.ARN, tc.wantARN)
			}
			if obs.ResourceID != tc.wantARN {
				t.Fatalf("node ResourceID = %q, want %q", obs.ResourceID, tc.wantARN)
			}
			anchored := false
			for _, anchor := range obs.CorrelationAnchors {
				if anchor == tc.wantARN {
					anchored = true
				}
			}
			if !anchored {
				t.Fatalf("CorrelationAnchors %v missing partition-aware ARN %q", obs.CorrelationAnchors, tc.wantARN)
			}
		})
	}
}

// TestLoggingRelationshipDerivesPartition pins that the bucket->bucket logging
// edge endpoints inherit the boundary partition, so a GovCloud/China source and
// target both resolve to their partition-correct bucket nodes.
func TestLoggingRelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name       string
		region     string
		wantSource string
		wantTarget string
	}{
		{name: "commercial", region: "us-east-1", wantSource: "arn:aws:s3:::src", wantTarget: "arn:aws:s3:::dst"},
		{name: "govcloud", region: "us-gov-west-1", wantSource: "arn:aws-us-gov:s3:::src", wantTarget: "arn:aws-us-gov:s3:::dst"},
		{name: "china", region: "cn-north-1", wantSource: "arn:aws-cn:s3:::src", wantTarget: "arn:aws-cn:s3:::dst"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bucket := Bucket{Name: "src"}
			bucket.Logging.TargetBucket = "dst"
			obs, ok := loggingRelationship(awscloud.Boundary{Region: tc.region}, bucket)
			if !ok {
				t.Fatalf("expected a logging relationship")
			}
			if obs.SourceARN != tc.wantSource {
				t.Fatalf("source_arn = %q, want %q", obs.SourceARN, tc.wantSource)
			}
			if obs.TargetARN != tc.wantTarget {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.wantTarget)
			}
		})
	}
}
