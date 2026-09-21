// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lakeformation

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// partition returns the AWS partition for a scan boundary's region. It mirrors
// the per-scanner helper Glue uses so a synthesized ARN inherits the boundary's
// partition (aws / aws-us-gov / aws-cn) instead of a hardcoded commercial
// value, keeping GovCloud and China graph joins from dangling. Registered
// Lake Formation locations carry an S3 ARN whose partition is preferred over
// the boundary; this helper is the fallback when no source ARN is available.
func partition(boundary awscloud.Boundary) string {
	return awscloud.PartitionForBoundary(boundary)
}

// bucketARNFromS3LocationARN extracts the bucket-scoped S3 ARN
// (`arn:<partition>:s3:::<bucket>`) from a registered Lake Formation data
// location ARN such as `arn:aws:s3:::bucket/prefix`. The partition is taken from
// the source ARN itself so the synthesized bucket identity matches the real S3
// node's partition without consulting the region; the boundary partition is the
// fallback when the source ARN's partition segment is blank. It returns
// ok=false when the value is not an S3 location ARN or carries no bucket
// segment.
func bucketARNFromS3LocationARN(boundary awscloud.Boundary, locationARN string) (bucketARN string, bucket string, prefix string, ok bool) {
	trimmed := strings.TrimSpace(locationARN)
	if !strings.HasPrefix(trimmed, "arn:") {
		return "", "", "", false
	}
	// arn:<partition>:s3:::<bucket>[/<prefix>]
	parts := strings.SplitN(trimmed, ":", 6)
	if len(parts) < 6 {
		return "", "", "", false
	}
	if strings.TrimSpace(parts[2]) != "s3" {
		return "", "", "", false
	}
	resource := strings.TrimSpace(parts[5])
	if resource == "" {
		return "", "", "", false
	}
	if slash := strings.IndexByte(resource, '/'); slash >= 0 {
		bucket = strings.TrimSpace(resource[:slash])
		prefix = strings.TrimSpace(resource[slash+1:])
	} else {
		bucket = resource
	}
	if bucket == "" {
		return "", "", "", false
	}
	partitionValue := strings.TrimSpace(parts[1])
	if partitionValue == "" {
		partitionValue = partition(boundary)
	}
	bucketARN = "arn:" + partitionValue + ":s3:::" + bucket
	return bucketARN, bucket, prefix, true
}

// glueTableResourceID builds the `database/table` identifier the Glue scanner
// publishes as its table resource_id, so a Lake Formation permission-to-table
// edge joins the Glue table node. It returns the bare database name when only
// the database is known and an empty string when neither is set.
func glueTableResourceID(databaseName, tableName string) string {
	databaseName = strings.TrimSpace(databaseName)
	tableName = strings.TrimSpace(tableName)
	switch {
	case databaseName != "" && tableName != "":
		return databaseName + "/" + tableName
	case databaseName != "":
		return databaseName
	default:
		return ""
	}
}

// isRoleARN reports whether value is an IAM role ARN (`arn:<partition>:iam::...
// :role/...`). Lake Formation principal identifiers may be IAM ARNs, account
// ids, or special principals such as IAM_ALLOWED_PRINCIPALS; only role ARNs
// join the IAM-role node, so the principal edge is emitted only for them.
func isRoleARN(value string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return false
	}
	return strings.Contains(trimmed, ":iam:") && strings.Contains(trimmed, ":role/")
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
