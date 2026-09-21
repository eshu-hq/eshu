// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package glue

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestTableS3LocationRelationshipDerivesPartition pins the GovCloud/China
// graph-join contract: the synthesized S3 bucket ARN must inherit the partition
// of the scan boundary's region, not a hardcoded commercial `aws`. Glue tables
// carry no ARN, so the boundary region is the partition source. The S3 bucket
// scanner publishes its resource_id as `arn:<partition>:s3:::<bucket>`, so a
// hardcoded partition silently dangles the table->S3-location edge in
// aws-us-gov and aws-cn.
func TestTableS3LocationRelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:s3:::lakehouse"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:s3:::lakehouse"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:s3:::lakehouse"},
		{name: "blank region falls back to commercial", region: "", want: "arn:aws:s3:::lakehouse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region}
			table := Table{
				Name:            "orders",
				DatabaseName:    "analytics",
				StorageLocation: "s3://lakehouse/orders/",
			}
			obs := tableS3LocationRelationship(boundary, table)
			if obs == nil {
				t.Fatalf("tableS3LocationRelationship returned nil for a valid s3 location")
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
