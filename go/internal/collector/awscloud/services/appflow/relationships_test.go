// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestFlowS3RelationshipDerivesPartition pins the GovCloud/China graph-join
// contract for the only ARN this scanner synthesizes: the S3 bucket ARN.
// AppFlow's S3 connector reports a bare bucket name, so the bucket ARN's
// partition must inherit from the flow ARN observed in the same describe
// response (falling back to the boundary region when the flow ARN is absent).
// The S3 bucket scanner publishes its resource_id as
// `arn:<partition>:s3:::<bucket>`, so a hardcoded `aws` partition silently
// dangles the flow->S3 edge in aws-us-gov and aws-cn.
func TestFlowS3RelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name    string
		flowARN string
		region  string
		want    string
	}{
		{
			name:    "commercial from flow arn",
			flowARN: "arn:aws:appflow:us-east-1:123456789012:flow/f",
			region:  "us-east-1",
			want:    "arn:aws:s3:::landing",
		},
		{
			name:    "govcloud from flow arn",
			flowARN: "arn:aws-us-gov:appflow:us-gov-west-1:123456789012:flow/f",
			region:  "us-gov-west-1",
			want:    "arn:aws-us-gov:s3:::landing",
		},
		{
			name:    "china from flow arn",
			flowARN: "arn:aws-cn:appflow:cn-north-1:123456789012:flow/f",
			region:  "cn-north-1",
			want:    "arn:aws-cn:s3:::landing",
		},
		{
			name:    "no flow arn falls back to govcloud region",
			flowARN: "",
			region:  "us-gov-west-1",
			want:    "arn:aws-us-gov:s3:::landing",
		},
		{
			name:    "no flow arn blank region falls back to commercial",
			flowARN: "",
			region:  "",
			want:    "arn:aws:s3:::landing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region}
			flow := Flow{
				ARN:            tc.flowARN,
				Name:           "f",
				SourceS3Bucket: "landing",
			}
			obs := flowS3SourceRelationship(boundary, flow)
			if obs == nil {
				t.Fatalf("flowS3SourceRelationship returned nil for a valid S3 source bucket")
			}
			if obs.TargetResourceID != tc.want {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.want)
			}
			if obs.TargetARN != tc.want {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.want)
			}
			if obs.TargetType != awscloud.ResourceTypeS3Bucket {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeS3Bucket)
			}
		})
	}
}

// TestIsSecretsManagerARN pins the exact-service-segment match the
// connector-profile-to-secret edge depends on, including the GovCloud/China
// partitions and the substring-containment trap.
func TestIsSecretsManagerARN(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "commercial secret", value: "arn:aws:secretsmanager:us-east-1:123456789012:secret:appflow!x-Ab3", want: true},
		{name: "govcloud secret", value: "arn:aws-us-gov:secretsmanager:us-gov-west-1:123456789012:secret:x", want: true},
		{name: "china secret", value: "arn:aws-cn:secretsmanager:cn-north-1:123456789012:secret:x", want: true},
		{name: "iam role with substring", value: "arn:aws:iam::123456789012:role/secretsmanager-access", want: false},
		{name: "kms key", value: "arn:aws:kms:us-east-1:123456789012:key/abcd", want: false},
		{name: "bare name", value: "my-secret", want: false},
		{name: "empty", value: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSecretsManagerARN(tc.value); got != tc.want {
				t.Fatalf("isSecretsManagerARN(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
