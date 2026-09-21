// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import "testing"

func TestPartitionForRegion(t *testing.T) {
	cases := []struct {
		region string
		want   string
	}{
		{"us-east-1", PartitionAWS},
		{"eu-west-3", PartitionAWS},
		{"", PartitionAWS},
		{"us-gov-west-1", PartitionGovCloud},
		{"us-gov-east-1", PartitionGovCloud},
		{"cn-north-1", PartitionChina},
		{"cn-northwest-1", PartitionChina},
		{"  us-gov-west-1  ", PartitionGovCloud},
	}
	for _, tc := range cases {
		if got := PartitionForRegion(tc.region); got != tc.want {
			t.Fatalf("PartitionForRegion(%q) = %q, want %q", tc.region, got, tc.want)
		}
	}
}

func TestPartitionForBoundary(t *testing.T) {
	if got := PartitionForBoundary(Boundary{Region: "us-gov-west-1"}); got != PartitionGovCloud {
		t.Fatalf("PartitionForBoundary govcloud = %q, want %q", got, PartitionGovCloud)
	}
	if got := PartitionForBoundary(Boundary{Region: "us-east-1"}); got != PartitionAWS {
		t.Fatalf("PartitionForBoundary commercial = %q, want %q", got, PartitionAWS)
	}
}

func TestPartitionFromARN(t *testing.T) {
	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:s3:::bucket", PartitionAWS},
		{"arn:aws-us-gov:sagemaker:us-gov-west-1:123:model/m", PartitionGovCloud},
		{"arn:aws-cn:kms:cn-north-1:123:key/abc", PartitionChina},
		{"not-an-arn", PartitionAWS},
		{"", PartitionAWS},
		{"arn::s3:::bucket", PartitionAWS}, // empty partition segment falls back
	}
	for _, tc := range cases {
		if got := PartitionFromARN(tc.arn); got != tc.want {
			t.Fatalf("PartitionFromARN(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}
