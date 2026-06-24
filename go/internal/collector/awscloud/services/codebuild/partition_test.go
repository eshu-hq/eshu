// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codebuild

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestS3BucketARNFromLocationDerivesPartition pins that the synthesized S3 bucket
// ARN for a bare CodeBuild source/artifact location inherits the partition of
// the scan boundary's region instead of a hardcoded commercial `aws`. The S3
// bucket scanner publishes its resource_id as `arn:<partition>:s3:::<bucket>`,
// so a hardcoded partition silently dangles the project->S3 edge in aws-us-gov
// and aws-cn.
func TestS3BucketARNFromLocationDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:s3:::artifacts"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:s3:::artifacts"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:s3:::artifacts"},
		{name: "blank region falls back to commercial", region: "", want: "arn:aws:s3:::artifacts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region}
			if got := s3BucketARNFromLocation(boundary, "artifacts/path/key"); got != tc.want {
				t.Fatalf("s3BucketARNFromLocation(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

// TestS3BucketARNFromLocationPreservesObjectARNPartition pins that when the
// location is already an S3 object ARN, the reduction to the bucket ARN
// preserves the source partition (commercial, GovCloud, or China) rather than
// only matching the commercial `arn:aws:s3:::` prefix.
func TestS3BucketARNFromLocationPreservesObjectARNPartition(t *testing.T) {
	cases := []struct {
		name     string
		location string
		want     string
	}{
		{name: "commercial object arn", location: "arn:aws:s3:::bkt/key.zip", want: "arn:aws:s3:::bkt"},
		{name: "govcloud object arn", location: "arn:aws-us-gov:s3:::bkt/key.zip", want: "arn:aws-us-gov:s3:::bkt"},
		{name: "china object arn", location: "arn:aws-cn:s3:::bkt/key.zip", want: "arn:aws-cn:s3:::bkt"},
		{name: "china bucket arn unchanged", location: "arn:aws-cn:s3:::bkt", want: "arn:aws-cn:s3:::bkt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The boundary region is irrelevant when the location is already an
			// ARN; the partition is inherited from the source ARN.
			boundary := awscloud.Boundary{Region: "us-east-1"}
			if got := s3BucketARNFromLocation(boundary, tc.location); got != tc.want {
				t.Fatalf("s3BucketARNFromLocation(%q) = %q, want %q", tc.location, got, tc.want)
			}
		})
	}
}
