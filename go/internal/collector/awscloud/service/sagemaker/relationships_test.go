// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestModelArtifactRelationshipDerivesPartition pins the GovCloud/China
// graph-join contract: the synthesized S3 bucket ARN must inherit the partition
// of the model ARN that referenced it, not a hardcoded commercial `aws`. The S3
// bucket scanner publishes its resource_id as `arn:<partition>:s3:::<bucket>`,
// so a hardcoded partition silently dangles the model->artifact edge in
// aws-us-gov and aws-cn.
func TestModelArtifactRelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name     string
		modelARN string
		want     string
	}{
		{
			name:     "commercial",
			modelARN: "arn:aws:sagemaker:us-east-1:123456789012:model/m",
			want:     "arn:aws:s3:::artifacts",
		},
		{
			name:     "govcloud",
			modelARN: "arn:aws-us-gov:sagemaker:us-gov-west-1:123456789012:model/m",
			want:     "arn:aws-us-gov:s3:::artifacts",
		},
		{
			name:     "china",
			modelARN: "arn:aws-cn:sagemaker:cn-north-1:123456789012:model/m",
			want:     "arn:aws-cn:s3:::artifacts",
		},
		{
			name:     "missing model arn falls back to commercial",
			modelARN: "",
			want:     "arn:aws:s3:::artifacts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs, ok := modelArtifactRelationship(
				"model-id",
				tc.modelARN,
				"s3://artifacts/model.tar.gz",
				map[string]struct{}{},
			)
			if !ok {
				t.Fatalf("modelArtifactRelationship returned ok=false for a valid s3 artifact URL")
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
