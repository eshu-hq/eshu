// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package synthetics

import "testing"

// TestBucketNameFromArtifactLocation proves the bucket name is extracted from
// the leading path segment of a Synthetics artifact location, stripping any
// s3:// scheme and leading slash, so the synthesized S3 ARN keys the real bucket
// node instead of including the object prefix.
func TestBucketNameFromArtifactLocation(t *testing.T) {
	cases := []struct {
		name     string
		location string
		want     string
	}{
		{name: "bucket and prefix", location: "checkout-artifacts/canary/run", want: "checkout-artifacts"},
		{name: "bucket only", location: "checkout-artifacts", want: "checkout-artifacts"},
		{name: "s3 scheme", location: "s3://checkout-artifacts/canary", want: "checkout-artifacts"},
		{name: "leading slash", location: "/checkout-artifacts/canary", want: "checkout-artifacts"},
		{name: "padded", location: "  checkout-artifacts/canary  ", want: "checkout-artifacts"},
		{name: "empty", location: "", want: ""},
		{name: "scheme only", location: "s3://", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bucketNameFromArtifactLocation(tc.location); got != tc.want {
				t.Fatalf("bucketNameFromArtifactLocation(%q) = %q, want %q", tc.location, got, tc.want)
			}
		})
	}
}

// TestArnForBucketIsPartitionAware proves the synthesized bucket ARN carries the
// supplied partition and that an already-formed ARN passes through unchanged.
func TestArnForBucketIsPartitionAware(t *testing.T) {
	if got, want := arnForBucket("aws-cn", "cn-bucket"), "arn:aws-cn:s3:::cn-bucket"; got != want {
		t.Fatalf("arnForBucket(China) = %q, want %q", got, want)
	}
	if got := arnForBucket("aws", ""); got != "" {
		t.Fatalf("arnForBucket(empty) = %q, want empty", got)
	}
	preformed := "arn:aws:s3:::already"
	if got := arnForBucket("aws", preformed); got != preformed {
		t.Fatalf("arnForBucket(preformed) = %q, want %q", got, preformed)
	}
}
