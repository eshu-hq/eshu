// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mwaa

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// partition returns the AWS partition for the scan boundary's region. The
// scanner uses it only when it must synthesize an S3 bucket ARN from a bare
// bucket name, so the synthesized identity carries aws-us-gov or aws-cn in
// GovCloud and China instead of dangling against the commercial-partition
// bucket node. When the source data already carries an ARN, the scanner reads
// the partition from that ARN instead.
func partition(boundary awscloud.Boundary) string {
	return awscloud.PartitionForBoundary(boundary)
}

// s3BucketARN returns the S3 bucket ARN for an MWAA source-bucket reference.
// When the reference is already an ARN it is returned trimmed and unchanged so
// it inherits its own partition. When the reference is a bare bucket name the
// ARN is synthesized with the boundary partition. The empty string is returned
// when no bucket can be derived so the caller skips the edge.
func s3BucketARN(boundary awscloud.Boundary, sourceBucket string) string {
	trimmed := strings.TrimSpace(sourceBucket)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "arn:") {
		return trimmed
	}
	return "arn:" + partition(boundary) + ":s3:::" + trimmed
}

// trimLogGroupWildcardARN drops the trailing ":*" wildcard suffix MWAA appends
// to a CloudWatch Logs log group ARN. The cloudwatchlogs scanner publishes the
// non-wildcard ARN as its resource_id, so the suffix must be trimmed for the
// MWAA-to-log-group edge to join instead of dangling.
func trimLogGroupWildcardARN(arn string) string {
	return strings.TrimSuffix(strings.TrimSpace(arn), ":*")
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

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
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
