// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// bucketARN synthesizes the S3 bucket ARN for the claim region's partition. S3
// buckets have no ARN in the API response, so the adapter synthesizes one; it
// must carry the real partition (aws / aws-cn / aws-us-gov) because the bucket
// node identity is what partition-aware consumers join against. A blank region
// falls back to the commercial partition.
func bucketARN(region, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForRegion(region) + ":s3:::" + name
}
