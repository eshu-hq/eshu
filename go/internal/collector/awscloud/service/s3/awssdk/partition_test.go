// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import "testing"

// TestBucketARNDerivesPartition pins that the SDK adapter synthesizes the bucket
// ARN with the partition of the claim region, not a hardcoded commercial `aws`.
// This is the value the scanner publishes as the bucket node identity, so it
// must match what partition-aware consumers target in GovCloud and China.
func TestBucketARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		bucket string
		want   string
	}{
		{name: "commercial", region: "us-east-1", bucket: "b", want: "arn:aws:s3:::b"},
		{name: "govcloud", region: "us-gov-west-1", bucket: "b", want: "arn:aws-us-gov:s3:::b"},
		{name: "china", region: "cn-north-1", bucket: "b", want: "arn:aws-cn:s3:::b"},
		{name: "blank region falls back to commercial", region: "", bucket: "b", want: "arn:aws:s3:::b"},
		{name: "blank bucket is empty", region: "us-gov-west-1", bucket: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bucketARN(tc.region, tc.bucket); got != tc.want {
				t.Fatalf("bucketARN(%q, %q) = %q, want %q", tc.region, tc.bucket, got, tc.want)
			}
		})
	}
}
